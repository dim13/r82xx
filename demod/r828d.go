package demod

import "github.com/dim13/r82xx/wire"

// tunerFamilyAGCConstants are empirically-tuned demodulator AGC-loop
// gain constants matched to the R820T/R828D noise/gain profile (RFC
// Table 20). Applied to both chips even though §10.11's heading names
// only "R828D Detection": the RFC gives no separate R820T constant
// set, both chips present the same electrical characteristics to the
// demod front end, and the alternative (leaving R820T on the zero-IF
// power-on default) is wrong for either chip. Our reading, not a
// literal RFC statement. All page 1 unless noted.
var tunerFamilyAGCConstants = []struct {
	page, addr uint8
	val        uint16
	n          int
}{
	{1, 0x02, 0x40, 1},
	{1, 0x03, 0x80, 1},
	{1, 0x04, 0xcc, 1},
	{1, 0x05, 0xbe, 1},
	{1, 0x06, 0x35, 1},
	{1, 0x07, 0x40, 1},
	{1, 0x12, 0x5a, 1},
	{1, 0xc7, 0x24, 1},
	{1, 0xc8, 0x14, 1},
	{1, 0xc9, 0x21, 1},
	{1, 0xca, 0x21, 1},
	{1, 0xcb, 0x00, 1},
	{1, 0xcd, 0x10, 1},
	{1, 0xce, 0x10, 1},
	{0, 0x11, 0xe9f4, 2},
}

// ApplyR820TR828D applies the R820T/R828D demodulator reconfiguration
// (RFC §10.11): AGC-loop constants, then the ADC/IF-datapath switch
// off the power-on zero-IF default onto this tuner family's real
// 3.57MHz single-ADC-channel operating mode. MUST run unconditionally
// on every device open, immediately after tuner detection and before
// the tuner's own power-up sequence.
func ApplyR820TR828D(t wire.Transport, crystalHz float64) error {
	for _, c := range tunerFamilyAGCConstants {
		if err := demodWriteReg(t, c.page, c.addr, c.val, c.n); err != nil {
			return err
		}
	}
	if err := demodWriteReg(t, 1, 0xb1, 0x1a, 1); err != nil { // zero-IF off
		return err
	}
	if err := demodWriteReg(t, 0, 0x08, 0x4d, 1); err != nil { // I-ADC path only
		return err
	}
	if err := setIF(t, DefaultIFHz, crystalHz); err != nil {
		return err
	}
	return setSpectrumInversion(t, true)
}
