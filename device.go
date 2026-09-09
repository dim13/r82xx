// Package r82xx implements the RTL2832U + R828D USB SDR receiver as
// fitted to the RTL-SDR Blog V4, derived from docs/hardware-spec-rfc.txt.
package r82xx

import (
	"errors"
	"sync"

	"github.com/dim13/r82xx/demod"
	"github.com/dim13/r82xx/tuner"
	"github.com/dim13/r82xx/wire"
)

// Device is an opened RTL2832U + R820T/R828D receiver (RFC §14
// walkthrough).
type Device struct {
	t  wire.Transport
	mu sync.Mutex

	tuner  *tuner.Tuner
	blogV4 bool

	demodCrystalHz float64 // nominal, PPM-corrected
	ppm            float64

	lastFrequencyHz  float64 // last requested (pre-upconversion) frequency
	haveFrequency    bool
	harmonicOverride *int // R820T-only manual harmonic index, §9; nil = auto-select by frequency
}

// Open runs Phase A of the end-to-end walkthrough (RFC §6, §14.1):
// baseband bring-up, tuner detection, and demodulator reconfiguration
// and tuner power-up for whichever of the R820T/R828D family answered
// -- this package's only implemented tuner family (RFC §1 scope: the
// other candidates tuner.Detect() also probes are out of scope past
// detection).
func Open(t wire.Transport) (*Device, error) {
	if err := demod.BasebandInit(t); err != nil {
		return nil, err
	}
	manufacturer, product, err := t.Strings()
	if err != nil {
		return nil, err
	}

	var typ tuner.Type
	err = withRepeaterKeepOpen(t, func() error {
		typ, err = tuner.Detect(t)
		return err
	})
	if err != nil {
		demod.RepeaterDisable(t)
		return nil, err
	}
	if typ != tuner.R828D && typ != tuner.R820T {
		demod.RepeaterDisable(t)
		if typ == tuner.None {
			return nil, errors.New("r82xx: no tuner responded")
		}
		return nil, tuner.ErrUnsupported
	}

	blogV4 := typ == tuner.R828D && manufacturer == "RTLSDRBlog" && product == "Blog V4"
	crystalHz := tuner.NominalCrystalHz(typ, blogV4)

	if err := demod.ApplyR820TR828D(t, crystalHz); err != nil {
		demod.RepeaterDisable(t)
		return nil, err
	}
	tu := tuner.New(t, typ, crystalHz, blogV4)
	if err := tu.PowerUp(); err != nil {
		demod.RepeaterDisable(t)
		return nil, err
	}
	if err := demod.RepeaterDisable(t); err != nil {
		return nil, err
	}

	return &Device{
		t:              t,
		tuner:          tu,
		blogV4:         blogV4,
		demodCrystalHz: demod.NominalCrystalHz,
	}, nil
}

// withRepeaterKeepOpen enables the repeater and runs fn, leaving the
// repeater enabled on success so Open's subsequent steps (which run
// "with the repeater still enabled", RFC §6 steps 6-8) don't need to
// re-enable it. The caller is responsible for eventually disabling it.
func withRepeaterKeepOpen(t wire.Transport, fn func() error) error {
	if err := demod.RepeaterEnable(t); err != nil {
		return err
	}
	return fn()
}

// Close runs Phase F (RFC §10.9, §7.1 deinit, §14.6): tuner standby
// over the repeater, then demodulator/ADC power-down. Callers MUST
// stop any streaming (Read/ReadAsync) before calling Close.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	err := demod.WithRepeater(d.t, d.tuner.Standby)
	if perr := demod.PowerDown(d.t); err == nil {
		err = perr
	}
	return err
}

// SetSampleRate sets the demodulator resampler ratio (RFC §7.5),
// rewriting the PPM fine trim and re-triggering a soft reset.
func (d *Device) SetSampleRate(hz uint32) (achievedHz float64, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return demod.SetSampleRate(d.t, d.demodCrystalHz, d.ppm, hz)
}

// SetPPM applies frequency-correction PPM to both the demodulator and
// tuner reference crystals (RFC §7.4). Retune (SetFrequency) after
// changing this if a frequency is already set, to apply the corrected
// tuner crystal to the PLL math.
func (d *Device) SetPPM(ppm float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ppm = ppm
	nominalTuner := tuner.NominalCrystalHz(d.tuner.Chip, d.blogV4)
	d.tuner.CrystalHz = demod.CorrectedHz(nominalTuner, ppm)
	return demod.SetPPM(d.t, ppm)
}

// SetFrequency tunes to requestedHz (RFC §14.3 Phase C): applies Blog
// V4 HF upconversion, derives the target LO frequency from the current
// sideband/bandwidth/IF-offset state, programs the RF band table and
// PLL (retrying in harmonic mode if needed), then applies board input
// switching, all bracketed by one repeater enable/disable.
func (d *Device) SetFrequency(requestedHz float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	err := demod.WithRepeater(d.t, func() error { return d.tuneLocked(requestedHz) })
	if err == nil {
		d.lastFrequencyHz = requestedHz
		d.haveFrequency = true
	}
	return err
}

func (d *Device) tuneLocked(requestedHz float64) error {
	eff := requestedHz
	if d.blogV4 {
		eff = tuner.UpconvertHF(requestedHz)
	}

	manual := d.harmonicOverride != nil
	order := 1
	switch {
	case manual:
		order = max(*d.harmonicOverride, 1)
	case eff > 1_770_000_000:
		order = 5
	}
	desiredLO, program := d.tuner.LOTargets(eff, order)

	if err := d.tuner.ApplyBand(desiredLO); err != nil {
		return err
	}
	if err := d.tuner.LO(program); err != nil {
		return err
	}
	locked, err := d.tuner.Locked()
	if err != nil {
		return err
	}
	if !locked && !manual && order == 1 && desiredLO > 1_760_000_000 {
		order = 5
		desiredLO, program = d.tuner.LOTargets(eff, order)
		if err := d.tuner.ApplyBand(desiredLO); err != nil {
			return err
		}
		if err := d.tuner.LO(program); err != nil {
			return err
		}
	}

	if d.blogV4 {
		if err := d.tuner.ApplyNotch(requestedHz); err != nil {
			return err
		}
		return d.tuner.ApplyInputSwitch(requestedHz)
	}
	return d.tuner.ApplyPlainInputSwitch(requestedHz)
}

// SetSideband selects upper/lower sideband for the tuner's IF
// conversion, then recomputes and reapplies the IF center frequency
// and currently tuned RF frequency (RFC §9 "Set sideband").
func (d *Device) SetSideband(upper bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tuner.SidebandUpper = upper
	if !d.haveFrequency {
		return nil
	}
	return demod.WithRepeater(d.t, func() error { return d.tuneLocked(d.lastFrequencyHz) })
}

// SetIFPassbandOffset shifts the tuner's IF filter center relative to
// nominal (RFC §9), then reapplies the currently tuned frequency.
func (d *Device) SetIFPassbandOffset(hz float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tuner.IFBandOffsetHz = hz
	if !d.haveFrequency {
		return nil
	}
	return demod.WithRepeater(d.t, func() error { return d.tuneLocked(d.lastFrequencyHz) })
}

// SetBandwidth applies a channel-filter bandwidth request; 0 falls
// back to the current sample rate (RFC §9). It reapplies the
// currently tuned frequency afterward, since bandwidth changes the
// delivered IF center that frequency programming depends on.
func (d *Device) SetBandwidth(hz float64, currentSampleRateHz float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if hz == 0 {
		hz = currentSampleRateHz
	}
	err := demod.WithRepeater(d.t, func() error {
		if _, err := d.tuner.SetBandwidth(hz); err != nil {
			return err
		}
		if d.haveFrequency {
			return d.tuneLocked(d.lastFrequencyHz)
		}
		return nil
	})
	return err
}

// SetGain applies one overall RF gain value from GainSteps (RFC §9
// "Set gain (composite)").
func (d *Device) SetGain(tenthsDB int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return demod.WithRepeater(d.t, func() error { return d.tuner.SetGain(tenthsDB) })
}

// ErrR828DOnly is returned by ops RFC §9 Table 7 labels R828D-specific
// ("Set gain, extended (3-way)", "Set IF mode") on an R820T tuner.
var ErrR828DOnly = errors.New("r82xx: operation restricted to R828D tuners (RFC §9)")

// SetExtendedGain sets LNA, mixer, and VGA independently (RFC §9 "Set
// gain, extended (3-way)"). R828D only.
func (d *Device) SetExtendedGain(lna, mixer, vga int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tuner.Chip != tuner.R828D {
		return ErrR828DOnly
	}
	return demod.WithRepeater(d.t, func() error { return d.tuner.SetExtendedGain(lna, mixer, vga) })
}

// SetIFMode applies the IF-gain host-API range encoding (RFC §9 "Set
// IF mode"). R828D only.
func (d *Device) SetIFMode(value int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tuner.Chip != tuner.R828D {
		return ErrR828DOnly
	}
	return demod.WithRepeater(d.t, func() error { return d.tuner.SetIFMode(value) })
}

// SetIFGain sets the VGA gain code directly (RFC §9 "Set IF gain
// (staged)").
func (d *Device) SetIFGain(code int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return demod.WithRepeater(d.t, func() error { return d.tuner.SetVGAGain(code) })
}

// SetGainAuto and SetGainManual toggle hardware-AGC vs. manual gain
// control.
func (d *Device) SetGainAuto() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return demod.WithRepeater(d.t, d.tuner.SetGainAuto)
}

// SetGainManual switches to manual gain control.
func (d *Device) SetGainManual() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return demod.WithRepeater(d.t, d.tuner.SetGainManual)
}

// Gain reads back the currently applied composite gain as mixer/LNA
// codes (RFC §9 "Get gain (composite)").
func (d *Device) Gain() (mixer, lna int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	err = demod.WithRepeater(d.t, func() error {
		var e error
		mixer, lna, e = d.tuner.GainReadback()
		return e
	})
	return mixer, lna, err
}

// PLLLocked reads back the tuner's PLL-lock status bit (RFC §9 "PLL
// lock query").
func (d *Device) PLLLocked() (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var locked bool
	err := demod.WithRepeater(d.t, func() error {
		var e error
		locked, e = d.tuner.Locked()
		return e
	})
	return locked, err
}

// RawRegister is the escape-hatch masked raw tuner register
// read/write (RFC §9 "Raw register access").
func (d *Device) RawRegister(reg, mask, val byte, write bool) (byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !write {
		return d.tuner.RawRead(reg, mask), nil
	}
	return 0, demod.WithRepeater(d.t, func() error { return d.tuner.RawWrite(reg, mask, val) })
}

// OverrideRegister pins mask bits of a tuner register to val across
// every future write (RFC §9 "Register override (masked)").
func (d *Device) OverrideRegister(reg, mask, val byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return demod.WithRepeater(d.t, func() error { return d.tuner.Override(reg, mask, val) })
}

// ClearOverrideRegister removes a pinned override.
func (d *Device) ClearOverrideRegister(reg byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tuner.ClearOverride(reg)
}

// ErrR820TOnly is returned by ops RFC §9 Table 7 gates to plain R820T
// (dithering enable, harmonic-mixing override) even on an R828D whose
// register and mechanism are identical -- kept rather than lifted.
var ErrR820TOnly = errors.New("r82xx: operation restricted to plain R820T tuners (RFC §9)")

// SetDitheringEnabled and SetDitheringDisabled set the PLL dithering
// bit (RFC §9 "Set PLL dithering enable"). R820T only.
func (d *Device) SetDitheringEnabled() error {
	return d.setDithering(d.tuner.SetDitheringEnabled)
}

// SetDitheringDisabled clears the PLL dithering bit.
func (d *Device) SetDitheringDisabled() error {
	return d.setDithering(d.tuner.SetDitheringDisabled)
}

func (d *Device) setDithering(fn func() error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tuner.Chip != tuner.R820T {
		return ErrR820TOnly
	}
	return demod.WithRepeater(d.t, fn)
}

// SetHarmonicOverride manually forces the harmonic-mixing index (RFC
// §9 "Set harmonic-mixing order override"; 0 or 1 = fundamental)
// instead of letting SetFrequency auto-select it by frequency
// (RFC §10.3 step 1), then reapplies the currently tuned frequency.
// Pass a negative order to clear the override and return to
// auto-select. R820T only.
func (d *Device) SetHarmonicOverride(order int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tuner.Chip != tuner.R820T {
		return ErrR820TOnly
	}
	if order < 0 {
		d.harmonicOverride = nil
	} else {
		d.harmonicOverride = &order
	}
	if !d.haveFrequency {
		return nil
	}
	return demod.WithRepeater(d.t, func() error { return d.tuneLocked(d.lastFrequencyHz) })
}
