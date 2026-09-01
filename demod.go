package r82xx

import "math"

// Demodulator paged-register access (RFC §5.4): wValue = (addr<<8)|0x20,
// wIndex = page (read) or 0x10|page (write). Every write MUST be
// followed by a fixed dummy read (page 0x0A, address 0x01) as a
// commit/flush step.

func demodReadReg(t Transport, page, addr uint8, n int) (uint16, error) {
	wValue := uint16(addr)<<8 | 0x20
	buf := make([]byte, n)
	if _, err := t.ControlIn(wValue, uint16(page), buf); err != nil {
		return 0, err
	}
	if n == 1 {
		return uint16(buf[0]), nil
	}
	return uint16(buf[0]) | uint16(buf[1])<<8, nil
}

func demodWriteReg(t Transport, page, addr uint8, val uint16, n int) error {
	wValue := uint16(addr)<<8 | 0x20
	buf := make([]byte, n)
	if n == 1 {
		buf[0] = byte(val)
	} else {
		buf[0], buf[1] = byte(val>>8), byte(val)
	}
	if err := t.ControlOut(wValue, 0x10|uint16(page), buf); err != nil {
		return err
	}
	_, err := demodReadReg(t, 0x0a, 0x01, 1) // required commit/flush read
	return err
}

// defaultFIRCoeffs is a built-in default channel FIR tuned generically
// for FM/DAB-style bandwidths (RFC §7.2). Coefficients are a tunable
// receive-channel-shape parameter, not a fixed hardware constant.
var defaultFIRCoeffs = [16]int16{
	-54, -36, -41, -40, -32, -14, 14, 53,
	101, 156, 215, 273, 327, 372, 404, 421,
}

// writeFIR packs 16 taps of a symmetric 32-tap decimation filter (the
// chip mirrors the second half in hardware) and writes them as 20
// bytes to page 1, addresses 0x1C-0x2F: the first 8 taps as signed
// 8-bit values, the remaining 8 as signed 12-bit values packed two per
// three bytes.
func writeFIR(t Transport, taps [16]int16) error {
	var buf [20]byte
	for i := range 8 {
		buf[i] = byte(int8(taps[i]))
	}
	for i := 0; i < 8; i += 2 {
		a, b := taps[8+i], taps[8+i+1]
		off := 8 + (i/2)*3
		buf[off] = byte(a >> 4)
		buf[off+1] = byte((a << 4) | ((b >> 8) & 0xf))
		buf[off+2] = byte(b)
	}
	for i, v := range buf {
		if err := demodWriteReg(t, 1, uint8(0x1c+i), uint16(v), 1); err != nil {
			return err
		}
	}
	return nil
}

// Fixed bring-up sequence, run once at device open (RFC §7.1).
func basebandInit(t Transport) error {
	steps := []struct {
		blk  block
		addr uint16
		val  uint16
		n    int
	}{
		{blockSys, 0x00, 0x09, 1}, // liveness / system-control
		{blockUSB, 0x00, 0x0002, 2},
		{blockUSB, 0x00, 0x1002, 2},
	}
	for _, s := range steps {
		if err := writeReg(t, s.blk, s.addr, s.val, s.n); err != nil {
			return err
		}
	}
	if err := writeReg(t, blockSys, 0x01, 0x22, 1); err != nil { // demod-control-1
		return err
	}
	if err := writeReg(t, blockSys, 0x02, 0xe8, 1); err != nil { // demod-control: power on
		return err
	}

	demodSteps := []struct {
		page, addr uint8
		val        uint16
		n          int
	}{
		{1, 0x01, 0x14, 1}, // soft reset strobe set
		{1, 0x01, 0x10, 1}, // ... and released
		{1, 0x15, 0x00, 1}, // disable spectrum inversion
		{1, 0x16, 0x0000, 2},
	}
	for _, s := range demodSteps {
		if err := demodWriteReg(t, s.page, s.addr, s.val, s.n); err != nil {
			return err
		}
	}
	for addr := uint8(0x16); addr <= 0x1b; addr++ { // zero IF-shift registers
		if err := demodWriteReg(t, 1, addr, 0, 1); err != nil {
			return err
		}
	}
	if err := writeFIR(t, defaultFIRCoeffs); err != nil {
		return err
	}

	rest := []struct {
		page, addr uint8
		val        uint16
	}{
		{0, 0x19, 0x05}, // SDR passthrough on, internal digital AGC off
		{1, 0x93, 0xf0},
		{1, 0x94, 0x0f},
		{1, 0x11, 0x00}, // (unverified, §15) no measurable effect
		{0, 0x61, 0x60}, // disable DVB-T packet filter
		{0, 0x06, 0x80}, // default ADC I/Q routing
		{1, 0xb1, 0x1b}, // zero-IF + DC offset cancel + I/Q imbalance comp
		{0, 0x0d, 0x83}, // disable 4.096MHz aux test clock
	}
	for _, s := range rest {
		if err := demodWriteReg(t, s.page, s.addr, s.val, 1); err != nil {
			return err
		}
	}
	return nil
}

// demodPowerDown powers off the demodulator and ADCs (RFC §7.1 step 5,
// device-close direction: same register takes 0x20 on power-down).
func demodPowerDown(t Transport) error {
	return writeReg(t, blockSys, 0x02, 0x20, 1)
}

// mode values for page 0, address 0x19 (RFC §7.6): the same register
// used for "SDR mode" at boot.
const (
	modeNormal      = 0x05 // SDR mode on, test mode off, internal AGC off
	modeTest        = 0x03
	modeInternalAGC = 0x25
)

func setMode(t Transport, val uint16) error { return demodWriteReg(t, 0, 0x19, val, 1) }

// setIF programs the 22-bit two's-complement IF NCO phase-increment
// register (RFC §7.3):
//
//	reg = -round(ifHz * 2^22 / crystalHz)
//
// split MSB-first across page-1 registers 0x19 (bits 21:16, masked to
// 6 bits), 0x1A (bits 15:8) and 0x1B (bits 7:0). MUST be recomputed
// against the current PPM-corrected crystal value on every change.
func setIF(t Transport, ifHz float64, crystalHz float64) error {
	v := ifRegValue(ifHz, crystalHz)
	if err := demodWriteReg(t, 1, 0x19, uint16((v>>16)&0x3f), 1); err != nil {
		return err
	}
	if err := demodWriteReg(t, 1, 0x1a, uint16((v>>8)&0xff), 1); err != nil {
		return err
	}
	return demodWriteReg(t, 1, 0x1b, uint16(v&0xff), 1)
}

func ifRegValue(ifHz, crystalHz float64) int32 {
	v := -math.Round(ifHz * (1 << 22) / crystalHz)
	return int32(v) & 0x3fffff
}

func setSpectrumInversion(t Transport, invert bool) error {
	v := uint16(0)
	if invert {
		v = 1
	}
	return demodWriteReg(t, 1, 0x15, v, 1)
}

// RFC §7.4.
func correctedHz(nominalHz float64, ppm float64) float64 {
	return nominalHz * (1 + ppm/1e6)
}

// ppmTrim computes the 14-bit signed resampler fine-trim value (RFC
// §7.4), written low byte to page-1 address 0x3F and bits[13:8]
// (masked to 6 bits) to address 0x3E. MUST be rewritten alongside
// every sample-rate change.
func ppmTrim(ppm float64) int16 {
	return int16(math.Round(-ppm * (1 << 24) / 1e6))
}

func setPPM(t Transport, ppm float64) error {
	trim := uint16(ppmTrim(ppm))
	if err := demodWriteReg(t, 1, 0x3f, trim&0xff, 1); err != nil {
		return err
	}
	return demodWriteReg(t, 1, 0x3e, (trim>>8)&0x3f, 1)
}

const (
	nominalCrystalHz = 28_800_000
	defaultIFHz      = 3_570_000 // matches the tuner's own default filter-bandwidth center
)

// SampleRateRange are the only two valid input ranges for SetSampleRate
// (RFC §7.5); the sub-range (300_000, 900_000] is an unrepresentable
// resampler dead zone.
var (
	ErrSampleRateRange = sampleRateRangeError{}
)

type sampleRateRangeError struct{}

func (sampleRateRangeError) Error() string {
	return "sample rate must be in (225000, 300000] or (900000, 3200000] Hz"
}

func validSampleRate(hz uint32) bool {
	return (hz > 225_000 && hz <= 300_000) || (hz > 900_000 && hz <= 3_200_000)
}

// resamplerRatio computes the ratio register value and the achieved
// (rounded) rate for a requested sample rate against crystalHz (RFC
// §7.5).
func resamplerRatio(crystalHz float64, requestedHz uint32) (ratio uint32, achievedHz float64) {
	r := uint32(math.Floor(crystalHz * (1 << 22) / float64(requestedHz)))
	r &= 0x0fffFFFC
	real := r | ((r & 0x08000000) << 1)
	return r, crystalHz * (1 << 22) / float64(real)
}

// Every sample-rate change requires a fresh PPM trim and soft reset
// (RFC §7.5).
func setSampleRate(t Transport, crystalHz float64, ppm float64, requestedHz uint32) (achievedHz float64, err error) {
	if !validSampleRate(requestedHz) {
		return 0, ErrSampleRateRange
	}
	ratio, achieved := resamplerRatio(crystalHz, requestedHz)
	if err := demodWriteReg(t, 1, 0x9f, uint16(ratio>>16), 2); err != nil {
		return 0, err
	}
	if err := demodWriteReg(t, 1, 0xa1, uint16(ratio), 2); err != nil {
		return 0, err
	}
	if err := setPPM(t, ppm); err != nil {
		return 0, err
	}
	if err := demodWriteReg(t, 1, 0x01, 0x14, 1); err != nil { // soft reset
		return 0, err
	}
	if err := demodWriteReg(t, 1, 0x01, 0x10, 1); err != nil {
		return 0, err
	}
	return achieved, nil
}
