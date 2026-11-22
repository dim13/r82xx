package demod

import "github.com/dim13/r82xx/wire"

// ApplyR820TR828D switches the demodulator off its power-on zero-IF
// default onto this tuner family's single-ADC-channel, 3.57MHz IF
// operating mode (RFC §10.11). MUST run on every device open,
// immediately after tuner detection and before the tuner's own
// power-up sequence. The DVB-T AGC-loop constants of RFC Table 20 are
// deliberately not written: the reference driver doesn't in SDR mode.
func ApplyR820TR828D(t wire.Transport, crystalHz float64) error {
	if err := demodWriteReg(t, 1, 0xb1, 0x1a, 1); err != nil { // zero-IF off
		return err
	}
	if err := demodWriteReg(t, 0, 0x08, 0x4d, 1); err != nil { // I-ADC path only
		return err
	}
	if err := SetIF(t, DefaultIFHz, crystalHz); err != nil {
		return err
	}
	return setSpectrumInversion(t, true)
}
