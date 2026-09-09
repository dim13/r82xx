package r82xx

import "errors"

// TunerType identifies which tuner family answered during detection
// (RFC §8, Table 6).
type TunerType int

const (
	TunerNone TunerType = iota
	TunerE4000
	TunerFC0013
	TunerR820T
	TunerR828D
	TunerFC2580
	TunerFC0012
)

// ErrUnsupportedTuner is returned when detection finds a tuner family
// whose register map is out of scope for this package (only R820T and
// R828D are implemented past detection; RFC §1 scope note).
var ErrUnsupportedTuner = errors.New("r82xx: detected tuner family is not implemented")

type tunerCandidate struct {
	typ      TunerType
	addr7    uint8
	checkReg uint8
	want     uint8
	mask     uint8
}

// candidates is the fixed detection priority order (RFC Table 6). A
// GPIO reset pulse for the remaining candidates would fire between
// R828D and FC2580 if nothing has matched yet; skipped here since it
// needs caller-supplied GPIORegs (§7.9) and is moot once R828D (this
// package's target) matches first.
var candidates = []tunerCandidate{
	{TunerE4000, 0x64, 0x02, 0x40, 0xff},
	{TunerFC0013, 0x63, 0x00, 0xa3, 0xff},
	{TunerR820T, 0x1a, 0x00, 0x69, 0xff},
	{TunerR828D, 0x3a, 0x00, 0x69, 0xff},
	{TunerFC2580, 0x56, 0x01, 0x56, 0x7f},
	{TunerFC0012, 0x63, 0x00, 0xa1, 0xff},
}

// detectTuner runs the fixed-order probe of RFC §8 with the I2C
// repeater held enabled for the whole sequence, stopping at the first
// match. It returns TunerNone if nothing answered.
//
// A per-candidate I2C error is treated as "not this one" and skipped,
// not as detection failure: an unanswered address commonly surfaces
// as a transfer error rather than a clean empty read, but the RFC
// doesn't characterize this precisely enough to assert it as fact.
func detectTuner(t Transport) (TunerType, error) {
	for _, c := range candidates {
		b, err := i2cRead(t, c.addr7, c.checkReg, 1)
		if err != nil {
			continue
		}
		if b[0]&c.mask == c.want {
			return c.typ, nil
		}
	}
	return TunerNone, nil
}
