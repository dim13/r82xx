package demod

import "github.com/dim13/r82xx/wire"

// DirectSamplingMode selects which ADC input direct sampling reads
// from, bypassing the tuner's IF output entirely (RFC §7.7).
type DirectSamplingMode int

const (
	DirectSamplingOff DirectSamplingMode = iota
	DirectSamplingI                      // I-ADC direct
	DirectSamplingQ                      // Q-ADC direct
)

// EnableDirectSampling runs steps 2-5 of the enable sequence; shutting
// down an attached tuner (step 1) and re-applying the tuned frequency
// (step 6) are the caller's (Device's) responsibility, since they
// cross into tuner/frequency-bookkeeping territory.
func EnableDirectSampling(t wire.Transport, mode DirectSamplingMode) error {
	if err := demodWriteReg(t, 1, 0xb1, 0x1a, 1); err != nil { // zero-IF off
		return err
	}
	if err := demodWriteReg(t, 1, 0x15, 0x00, 1); err != nil { // spectrum inversion off
		return err
	}
	if err := demodWriteReg(t, 0, 0x08, 0x4d, 1); err != nil { // I-ADC input path only
		return err
	}
	route := uint16(0x80) // I-ADC direct
	if mode == DirectSamplingQ {
		route = 0x90
	}
	return demodWriteReg(t, 0, 0x06, route, 1)
}

// DisableDirectSamplingR828D runs the R820T/R828D-family branch of the
// disable sequence (steps 2 and 4); re-running the tuner's power-up
// sequence (step 1) and re-applying the tuned frequency (step 5) are
// the caller's responsibility.
func DisableDirectSamplingR828D(t wire.Transport, crystalHz float64) error {
	if err := setIF(t, DefaultIFHz, crystalHz); err != nil {
		return err
	}
	if err := setSpectrumInversion(t, true); err != nil {
		return err
	}
	return demodWriteReg(t, 0, 0x06, 0x80, 1)
}

// DisableDirectSamplingOther runs the non-R820T/R828D-family branch.
func DisableDirectSamplingOther(t wire.Transport, crystalHz float64) error {
	if err := setIF(t, 0, crystalHz); err != nil {
		return err
	}
	if err := demodWriteReg(t, 0, 0x08, 0xcd, 1); err != nil {
		return err
	}
	if err := demodWriteReg(t, 1, 0xb1, 0x1b, 1); err != nil { // zero-IF back on
		return err
	}
	return demodWriteReg(t, 0, 0x06, 0x80, 1)
}

// OffsetHz computes offset tuning (RFC §7.8, 85% of the Nyquist
// half-rate); rejected outright for the R820T/R828D path and never
// called by Device, kept only for completeness against the wider
// device family.
func OffsetHz(sampleRateHz float64) float64 {
	return (sampleRateHz / 2) * 1.7
}
