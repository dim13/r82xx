package r82xx

import (
	"testing"

	"github.com/dim13/r82xx/tuner"
)

// smokeTransport is a stateful fake I2C bus, just enough to carry
// Device.Open/SetFrequency/Close through a full run without a real
// RTL2832U/R828D on the other end: the R828D chip-ID register answers
// correctly, and the PLL always reports locked.
type smokeTransport struct {
	manufacturer, product string
	i2c                   map[uint8]map[uint8]byte
	pendingReg            map[uint8]uint8
	demod                 map[uint16]byte // page<<8 | addr
	sys                   map[uint16]byte // system block, by address
}

func newSmokeTransport(manufacturer, product string) *smokeTransport {
	return &smokeTransport{
		manufacturer: manufacturer,
		product:      product,
		i2c: map[uint8]map[uint8]byte{
			0x3a: {
				0x00: 0x69, // chip-ID value (RFC Table 8), raw: detection reads this unreversed
				0x02: 0x02, // PLL locked (bit6): reverseByte(0x40)
				0x04: 0xa8, // VCO op-point 1, cal code 5: reverseByte(0x15)
			},
		},
		pendingReg: map[uint8]uint8{},
	}
}

func (s *smokeTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	if wIndex == 0x0200 && len(data) == 1 { // system block read
		data[0] = s.sys[wValue]
	}
	if wIndex&0xff00 == 0x0600 { // I2C block (wire.blockI2C)
		slave := uint8(wValue >> 1)
		reg := s.pendingReg[slave]
		regs := s.i2c[slave]
		for i := range data {
			data[i] = regs[reg+uint8(i)]
		}
	}
	return len(data), nil
}

func (s *smokeTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	if wIndex == 0x0210 && len(data) == 1 { // system block write
		if s.sys == nil {
			s.sys = map[uint16]byte{}
		}
		s.sys[wValue] = data[0]
	}
	if wValue&0xff == 0x20 && wIndex&0x10 != 0 && len(data) == 1 { // demod page write
		if s.demod == nil {
			s.demod = map[uint16]byte{}
		}
		s.demod[(wIndex&0x0f)<<8|wValue>>8] = data[0]
	}
	if wIndex&0xff00 == 0x0600 { // I2C block (wire.blockI2C)
		slave := uint8(wValue >> 1)
		if len(data) == 1 {
			s.pendingReg[slave] = data[0]
			return nil
		}
		if s.i2c[slave] == nil {
			s.i2c[slave] = map[uint8]byte{}
		}
		reg := data[0]
		for i, v := range data[1:] {
			s.i2c[slave][reg+uint8(i)] = v
		}
	}
	return nil
}

func (s *smokeTransport) BulkIn(data []byte) (int, error) {
	return len(data), nil
}

func (s *smokeTransport) Strings() (string, string, error) {
	return s.manufacturer, s.product, nil
}

func TestDeviceEndToEnd(t *testing.T) {
	tr := newSmokeTransport("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !d.blogV4 {
		t.Error("Open: expected Blog V4 board identification to succeed")
	}
	if d.tuner.CrystalHz != tuner.BlogV4CrystalHz {
		t.Errorf("tuner crystal = %v, want %v", d.tuner.CrystalHz, tuner.BlogV4CrystalHz)
	}

	if achieved, err := d.SetSampleRate(2_400_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	} else if diff := achieved - 2_400_000; diff > 1000 || diff < -1000 {
		t.Errorf("SetSampleRate achieved = %v, want ~2400000", achieved)
	}

	if err := d.SetFrequency(100_000_000); err != nil {
		t.Fatalf("SetFrequency: %v", err)
	}
	if !d.haveFrequency || d.lastFrequencyHz != 100_000_000 {
		t.Errorf("Device did not record the tuned frequency")
	}

	if err := d.SetGain(200); err != nil {
		t.Fatalf("SetGain: %v", err)
	}
	if err := d.SetGainAuto(); err != nil {
		t.Fatalf("SetGainAuto: %v", err)
	}
	if err := d.SetExtendedGain(1, 1, 1); err != nil {
		t.Errorf("SetExtendedGain on R828D: %v", err)
	}
	if err := d.SetIFMode(0); err != nil {
		t.Errorf("SetIFMode on R828D: %v", err)
	}
	if locked, err := d.PLLLocked(); err != nil || !locked {
		t.Errorf("PLLLocked = %v, %v, want true, nil", locked, err)
	}

	// A Blog V4 board pins VGA gain unconditionally (RFC §11.5).
	if err := d.SetIFGain(15); err != nil {
		t.Fatalf("SetIFGain: %v", err)
	}
	if got := d.tuner.RawRead(0x0c, 0x0f); got != 8 { // blogV4FixedVGACode
		t.Errorf("Blog V4 VGA code = %d, want pinned %d", got, 8)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpenPlainR828D(t *testing.T) {
	tr := newSmokeTransport("Generic", "RTL2838UHIDIR")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if d.blogV4 {
		t.Error("Open: expected plain-board identification (no Blog V4 match)")
	}
	if d.tuner.CrystalHz != tuner.R828DNominalCrystalHz {
		t.Errorf("tuner crystal = %v, want %v", d.tuner.CrystalHz, float64(tuner.R828DNominalCrystalHz))
	}
}

func newSmokeTransportR820T(manufacturer, product string) *smokeTransport {
	tr := newSmokeTransport(manufacturer, product)
	delete(tr.i2c, 0x3a)
	tr.i2c[0x1a] = map[uint8]byte{
		0x00: 0x69,
		0x02: 0x02, // PLL locked
		0x04: 0xa8, // VCO op-point 2 (bits5:4=01... see below), cal code 5
	}
	return tr
}

func TestOpenR820T(t *testing.T) {
	// Manufacturer/product strings deliberately match the Blog V4
	// board-id pair: Blog V4 boards only ever carry an R828D (RFC
	// §8), so an R820T match MUST NOT be treated as Blog V4 even if
	// the strings happen to match.
	tr := newSmokeTransportR820T("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if d.tuner.Chip != tuner.R820T {
		t.Fatalf("detected chip = %v, want tuner.R820T", d.tuner.Chip)
	}
	if d.blogV4 {
		t.Error("Open: R820T must never be identified as Blog V4")
	}
	if d.tuner.CrystalHz != tuner.R820TNominalCrystalHz {
		t.Errorf("tuner crystal = %v, want %v", d.tuner.CrystalHz, float64(tuner.R820TNominalCrystalHz))
	}

	if err := d.SetFrequency(100_000_000); err != nil {
		t.Fatalf("SetFrequency: %v", err)
	}

	// R820T-gated ops must work here...
	if err := d.SetDitheringDisabled(); err != nil {
		t.Errorf("SetDitheringDisabled on R820T: %v", err)
	}
	if err := d.SetHarmonicOverride(1); err != nil {
		t.Errorf("SetHarmonicOverride on R820T: %v", err)
	}
	// ...and R828D-gated ops must not.
	if err := d.SetExtendedGain(1, 1, 1); err != ErrR828DOnly {
		t.Errorf("SetExtendedGain on R820T err = %v, want ErrR828DOnly", err)
	}
	if err := d.SetIFMode(0); err != ErrR828DOnly {
		t.Errorf("SetIFMode on R820T err = %v, want ErrR828DOnly", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestR820TGatedOpsRejectedOnR828D(t *testing.T) {
	tr := newSmokeTransport("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := d.SetDitheringEnabled(); err != ErrR820TOnly {
		t.Errorf("SetDitheringEnabled on R828D err = %v, want ErrR820TOnly", err)
	}
	if err := d.SetHarmonicOverride(5); err != ErrR820TOnly {
		t.Errorf("SetHarmonicOverride on R828D err = %v, want ErrR820TOnly", err)
	}
}

func TestOpenNoTuner(t *testing.T) {
	tr := newSmokeTransport("Generic", "Nothing")
	delete(tr.i2c, 0x3a) // no chip answers
	if _, err := Open(tr); err == nil {
		t.Error("Open: expected an error when no tuner responds")
	}
}

// SetBandwidth must move the demodulator IF to where the tuner now
// delivers it. Expected values are what the reference driver writes
// at 1.024MS/s (IF 1.7MHz), captured on the wire.
func TestSetBandwidthUpdatesIF(t *testing.T) {
	tr := newSmokeTransport("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := d.SetBandwidth(0, 1_024_000); err != nil {
		t.Fatalf("SetBandwidth: %v", err)
	}
	want := map[uint16]byte{0x119: 0x3c, 0x11a: 0x38, 0x11b: 0xe4}
	for reg, w := range want {
		if got := tr.demod[reg]; got != w {
			t.Errorf("demod page 1 reg %#x = %#x, want %#x", reg&0xff, got, w)
		}
	}
}

// Bias tee drives GPIO 0 without disturbing the Blog V4 RF switch on
// GPIO 5, which tuning to VHF sets.
func TestSetBiasTee(t *testing.T) {
	tr := newSmokeTransport("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := d.SetFrequency(100_000_000); err != nil {
		t.Fatalf("SetFrequency: %v", err)
	}
	cases := []struct {
		on   bool
		want byte
	}{
		{on: true, want: 0x21},
		{on: false, want: 0x20},
	}
	for _, tc := range cases {
		if err := d.SetBiasTee(tc.on); err != nil {
			t.Fatalf("SetBiasTee(%v): %v", tc.on, err)
		}
		if got := tr.sys[0x3001]; got != tc.want { // GPO
			t.Errorf("SetBiasTee(%v): GPO = %#x, want %#x", tc.on, got, tc.want)
		}
		if got := tr.sys[0x3003]; got&0x01 == 0 { // GPOE
			t.Errorf("SetBiasTee(%v): GPIO 0 not an output, GPOE = %#x", tc.on, got)
		}
	}
}
