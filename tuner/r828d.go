// Package tuner drives the shared R820T/R828D register map and
// behavior (RFC §10: "except where a difference is called out
// explicitly, every register, field, and procedure... applies
// identically to both"), plus the RTL-SDR Blog V4 board-specific
// behavior layered on top of it (RFC §11).
package tuner

import (
	"errors"
	"math"

	"github.com/dim13/r82xx/demod"
	"github.com/dim13/r82xx/i2c"
	"github.com/dim13/r82xx/wire"
)

// I2C addresses and shared chip-ID check (RFC §10.1, §10.8): R820T
// and R828D are the same tuner core at two bus addresses, and answer
// the chip-ID check identically -- only the address disambiguates
// them.
const (
	r820tAddr7 = 0x1a
	r828dAddr7 = 0x3a

	chipIDReg = 0x00
	chipIDVal = 0x69
)

// Nominal reference crystal frequencies (RFC Table 9, Table 17):
// 16.0MHz plain R828D, 28.8MHz Blog V4 override, and R820T's
// board-dependent "typical" default, treated as a starting point, not
// an authoritative constant.
const (
	R820TNominalCrystalHz = 28_800_000
	R828DNominalCrystalHz = 16_000_000
	BlogV4CrystalHz       = 28_800_000
)

// NominalCrystalHz picks the nominal reference crystal for chip,
// applying the Blog V4 override when blogV4 is set (RFC Table 9,
// Table 17; §11.1).
func NominalCrystalHz(chip Type, blogV4 bool) float64 {
	if chip != R828D {
		return R820TNominalCrystalHz
	}
	if blogV4 {
		return BlogV4CrystalHz
	}
	return R828DNominalCrystalHz
}

// Tuner drives the shared R820T/R828D register map and behavior. Its
// registers 0x05-0x1F are shadow-cached: field updates read-modify-write
// the cache and issue one I2C write, without a round-trip read.
type Tuner struct {
	t         wire.Transport
	Chip      Type // R820T or R828D
	addr7     uint8
	vcoTarget byte // fine-VCO-trim target and PLL whole_part-bound divisor: 1 (R828D) or 2 (R820T), §10.3
	CrystalHz float64
	blogV4    bool

	regs      [32]byte
	overrides map[uint8]regOverride

	SidebandUpper     bool
	ifCenterHz        float64
	IFBandOffsetHz    float64
	lastInputBand     int  // -1 = unset; Blog V4 3-way switch cache, §11.4
	ditheringDisabled bool // R820T-only manual override, §9 "Set PLL dithering enable"
}

type regOverride struct {
	mask byte
	val  byte
}

// New constructs a Tuner for the given detected chip, bound to the
// given I2C transport and reference crystal. It performs no I2C
// traffic; call PowerUp to bring the chip up. blogV4 is forced false
// for chip == R820T: Blog V4 boards only ever carry an R828D (RFC §8).
func New(t wire.Transport, chip Type, crystalHz float64, blogV4 bool) *Tuner {
	addr7, vcoTarget := uint8(r828dAddr7), byte(1)
	if chip == R820T {
		addr7, vcoTarget, blogV4 = r820tAddr7, 2, false
	}
	return &Tuner{
		t:             t,
		Chip:          chip,
		addr7:         addr7,
		vcoTarget:     vcoTarget,
		CrystalHz:     crystalHz,
		blogV4:        blogV4,
		overrides:     make(map[uint8]regOverride),
		ifCenterHz:    demod.DefaultIFHz,
		lastInputBand: -1,
	}
}

func reverseByte(b byte) byte {
	b = b&0xf0>>4 | b&0x0f<<4
	b = b&0xcc>>2 | b&0x33<<2
	b = b&0xaa>>1 | b&0x55<<1
	return b
}

// readStatus reads one of the read-only status registers (0x00-0x04),
// un-reversing the bit order this chip family uses on its I2C read
// path (unverified, RFC §10 intro / §15 item 6).
//
// This chip family can only be read starting at register 0: the
// address byte written before a read is not honored as a random-access
// offset, so every status read is a burst from 0 up to and including
// reg, with the caller indexing the last byte. Confirmed against real
// Blog V4 hardware: passing reg directly as the read offset (as if
// random access were supported) silently returns register 0's content
// for any reg > 0, which is indistinguishable from a real read until
// something depending on it — PLL lock detection, VCO fine-trim —
// never sees the right bit.
func (tu *Tuner) readStatus(reg uint8) (byte, error) {
	b, err := i2c.Read(tu.t, tu.addr7, 0, int(reg)+1)
	if err != nil {
		return 0, err
	}
	return reverseByte(b[reg]), nil
}

// writeReg writes one register through the shadow cache, applying any
// pinned register-override mask (Table 7 "register override": a
// persistent clamp enforced centrally so every future write to that
// register keeps the pinned bits, unlike a one-off raw write).
func (tu *Tuner) writeReg(reg uint8, val byte) error {
	if ov, ok := tu.overrides[reg]; ok {
		val = val&^ov.mask | ov.val&ov.mask
	}
	tu.regs[reg] = val
	return i2c.Write(tu.t, tu.addr7, reg, []byte{val})
}

func (tu *Tuner) setField(reg uint8, mask byte, shift uint, val byte) error {
	b := tu.regs[reg]
	b = b&^mask | val<<shift&mask
	return tu.writeReg(reg, b)
}

// RawWrite is the escape-hatch "raw register access" operation
// (Table 7): writes val to reg, touching only the bits set in mask.
func (tu *Tuner) RawWrite(reg uint8, mask, val byte) error {
	return tu.setField(reg, mask, 0, val)
}

// RawRead is the escape-hatch masked register read.
func (tu *Tuner) RawRead(reg, mask uint8) byte {
	return tu.regs[reg] & mask
}

// Override pins mask bits of reg to val, enforced on every subsequent
// write until cleared (Table 7 "register override").
func (tu *Tuner) Override(reg, mask, val uint8) error {
	tu.overrides[reg] = regOverride{mask, val}
	return tu.writeReg(reg, tu.regs[reg])
}

// ClearOverride removes a pinned override.
func (tu *Tuner) ClearOverride(reg uint8) {
	delete(tu.overrides, reg)
}

// Detect confirms the chip-ID byte (RFC Table 8, Table 17: identical
// for R820T and R828D -- only the bus address disambiguates them).
func (tu *Tuner) Detect() (bool, error) {
	v, err := tu.readStatus(chipIDReg)
	if err != nil {
		return false, err
	}
	return v == chipIDVal, nil
}

// Locked reports the PLL lock flag (register 0x02 bit 6).
func (tu *Tuner) Locked() (bool, error) {
	v, err := tu.readStatus(0x02)
	if err != nil {
		return false, err
	}
	return v&0x40 != 0, nil
}

// ErrPLLUnreachable means the requested LO frequency is outside the
// synthesizer's divider/VCO range.
var ErrPLLUnreachable = errors.New("r82xx: requested local-oscillator frequency is unreachable")

// vcoRange bounds the VCO search in chooseDivider (RFC §10.3; §15
// item 7 flags the upper bound as uncertain).
var vcoRange = [2]float64{1.77e9, 3.9e9}

// chooseDivider returns the smallest divider (as a bit index 0-5,
// dividing by 2,4,8,...,64) placing loHz*divider inside the VCO range.
func chooseDivider(loHz float64) (index int, ok bool) {
	div := 2.0
	for i := range 6 {
		vco := loHz * div
		if vco >= vcoRange[0] && vco <= vcoRange[1] {
			return i, true
		}
		div *= 2
	}
	return 0, false
}

// LO programs the synthesizer to the given target local-oscillator
// frequency -- not a requested RF frequency; see Device.SetFrequency
// for the RF-to-LO derivation (RFC §10.3).
func (tu *Tuner) LO(loHz float64) error {
	divIdx, ok := chooseDivider(loHz)
	if !ok {
		return ErrPLLUnreachable
	}

	// Fastest auto-tune clock while acquiring lock.
	if err := tu.setField(0x1a, 0x0c, 2, 0b00); err != nil {
		return err
	}
	if err := tu.setField(0x10, 0xe0, 5, byte(divIdx)); err != nil {
		return err
	}

	// Fine VCO-band trim: nudge divider by one step if the reported
	// operating point misses the chip's target (1 for R828D, 2 for
	// plain R820T, RFC §10.3).
	vcoStat, err := tu.readStatus(0x04)
	if err != nil {
		return err
	}
	target := byte(tu.vcoTarget)
	if op := (vcoStat >> 4) & 0x3; op != target {
		if op > target && divIdx < 5 {
			divIdx++
		} else if op < target && divIdx > 0 {
			divIdx--
		}
		if err := tu.setField(0x10, 0xe0, 5, byte(divIdx)); err != nil {
			return err
		}
	}

	refHz := tu.CrystalHz
	if tu.regs[0x10]&0x10 != 0 { // RF2: reference divide-by-2
		refHz /= 2
	}
	divider := math.Pow(2, float64(divIdx+1))
	targetVCOHz := loHz * divider

	scaled := math.Round(targetVCOHz * 65536 / (2 * refHz))
	whole := uint32(scaled) / 65536
	frac := uint32(scaled) % 65536
	if maxWhole := uint32(128/tu.vcoTarget - 1); whole > maxWhole { // 127 (R828D) or 63 (R820T)
		return ErrPLLUnreachable
	}

	offset := int(whole) - 13
	if err := tu.writeReg(0x14, byte(offset/4)|byte(offset%4)<<6); err != nil {
		return err
	}
	if err := tu.writeReg(0x15, byte(frac)); err != nil {
		return err
	}
	if err := tu.writeReg(0x16, byte(frac>>8)); err != nil {
		return err
	}
	fracEnabled := frac != 0
	if err := tu.setField(0x12, 0x08, 3, boolBit(!fracEnabled)); err != nil { // bit3: 0=enabled
		return err
	}
	if fracEnabled && !tu.ditheringDisabled {
		if err := tu.setField(0x12, 0x10, 4, 0); err != nil { // dithering on by default
			return err
		}
	}

	locked, err := tu.Locked()
	if err != nil {
		return err
	}
	if !locked {
		if err := tu.setField(0x12, 0xe0, 5, 0b011); err != nil { // raise VCO bias, retry once
			return err
		}
		locked, err = tu.Locked()
		if err != nil {
			return err
		}
		if !locked {
			return errors.New("r82xx: PLL failed to lock")
		}
	}
	return tu.setField(0x1a, 0x0c, 2, 0b10) // slowest/most-accurate auto-tune clock once locked
}

// SetDitheringEnabled and SetDitheringDisabled set the PLL dithering
// bit (register 0x12 bit 4, RFC §9 "Set PLL dithering enable").
// Device.SetDitheringEnabled/Disabled gate this to R820T, matching the
// reference behavior's own restriction. A later LO() call re-enables
// dithering whenever fractional-N mode is active, unless this
// override was set first.
func (tu *Tuner) SetDitheringEnabled() error {
	return tu.setDithering(false)
}

func (tu *Tuner) SetDitheringDisabled() error {
	return tu.setDithering(true)
}

func (tu *Tuner) setDithering(disabled bool) error {
	tu.ditheringDisabled = disabled
	return tu.setField(0x12, 0x10, 4, boolBit(disabled))
}

func boolBit(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// bandRow is one entry of the 21-row RF front-end band table (RFC
// §10.4), selected by the highest StartMHz not exceeding the
// synthesizer's target LO frequency.
type bandRow struct {
	startMHz    float64
	notchLowZ   bool // RFC's "low"/"high" Open-Drain column, read as low-Z(1)/high-Z(0)
	trackBypass bool
	trackBand   byte // register field value: 0=highest, 1=mid, 2=low
	cornerByte  byte
}

// rfBandTable's per-band 20pF/10pF crystal-cap columns are omitted:
// the cap-mode selector is fixed at "0pF, high-drive" and never
// changed dynamically, so the 0pF column (0x00 for every row) is the
// only one ever read (RFC §10.4).
// ponytail: dormant 20pF/10pF cap table dropped; add back if dynamic
// cap-mode selection is ever implemented.
var rfBandTable = []bandRow{
	{0, true, false, 2, 0xdf},
	{50, true, false, 2, 0xbe},
	{55, true, false, 2, 0x8b},
	{60, true, false, 2, 0x7b},
	{65, true, false, 2, 0x69},
	{70, true, false, 2, 0x58},
	{75, false, false, 2, 0x44},
	{80, false, false, 2, 0x44},
	{90, false, false, 2, 0x34},
	{100, false, false, 2, 0x34},
	{110, false, false, 2, 0x24},
	{120, false, false, 2, 0x24},
	{140, false, false, 2, 0x14},
	{180, false, false, 2, 0x13},
	{220, false, false, 2, 0x13},
	{250, false, false, 2, 0x11},
	{280, false, false, 2, 0x00},
	{310, false, true, 1, 0x00},
	{450, false, true, 1, 0x00},
	{588, false, true, 0, 0x00},
	{650, false, true, 0, 0x00},
}

// LOTargets computes the target LO frequency (for RF-band-table
// lookup) and the value to actually program into the synthesizer for
// the given harmonic order (RFC §10.3 steps 2-4). Harmonic mixing
// flips sideband selection for orders 3, 7, 11, 15 (order mod 4 == 3).
func (tu *Tuner) LOTargets(effRFHz float64, order int) (desiredLOHz, programHz float64) {
	flip := order%4 == 3
	subtract := tu.SidebandUpper != flip
	sign := 1.0
	if subtract {
		sign = -1
	}
	desiredLOHz = effRFHz + sign*tu.ifCenterHz + tu.IFBandOffsetHz
	programHz = desiredLOHz
	if order > 1 {
		programHz = desiredLOHz / float64(order)
	}
	return desiredLOHz, programHz
}

func lookupBand(loHz float64) bandRow {
	mhz := loHz / 1e6
	row := rfBandTable[0]
	for _, r := range rfBandTable {
		if r.startMHz > mhz {
			break
		}
		row = r
	}
	return row
}

// ApplyBand writes the RF front-end band-table row for the given LO
// frequency (RFC §10.4). It only touches the tracking-filter/RF-band
// bits of 0x1A (AGC/PLL clock-rate bits are left as LO() set them),
// the corner byte 0x1B, the crystal-cap field of 0x10 (hardcoded
// 0x00), and the baseline open-drain/notch bit of 0x17 -- which Blog
// V4 boards additionally override per original (pre-upconversion)
// frequency, RFC §11.3.
func (tu *Tuner) ApplyBand(loHz float64) error {
	row := lookupBand(loHz)
	mode := byte(0)
	if row.trackBypass {
		mode = 1
	}
	if err := tu.setField(0x1a, 0xc0, 6, mode); err != nil {
		return err
	}
	if err := tu.setField(0x1a, 0x03, 0, row.trackBand); err != nil {
		return err
	}
	if err := tu.writeReg(0x1b, row.cornerByte); err != nil {
		return err
	}
	if err := tu.setField(0x10, 0x03, 0, 0x00); err != nil { // crystal cap: fixed 0pF
		return err
	}
	return tu.setField(0x17, 0x08, 3, boolBit(row.notchLowZ))
}
