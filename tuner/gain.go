package tuner

// Gain control (RFC §10.6): three independently switchable stages
// (LNA, mixer, IF/VGA), each automatic (chip AGC) or manual.

// lnaSteps and mixerSteps are relative-unit step tables in the
// reference behavior's own units, not confirmed absolute dB (RFC §15
// item 9). vgaSteps has a stated calibrated absolute dB scale.
var lnaSteps = [16]int{
	0, 9, 22, 62, 100, 113, 144, 166,
	192, 223, 249, 263, 282, 287, 322, 335,
}
var mixerSteps = [16]int{
	0, 5, 15, 25, 44, 53, 63, 88,
	105, 115, 123, 139, 152, 158, 161, 153,
}

// GainSteps is the fixed, hard-coded gain capability list (tenths of
// dB) reported to API callers (RFC Table 7); it involves no tuner I2C
// traffic.
var GainSteps = [29]int{
	0, 9, 14, 27, 37, 77, 87, 125,
	144, 157, 166, 197, 207, 229, 254, 280,
	297, 328, 338, 364, 372, 386, 402, 421,
	434, 439, 445, 480, 496,
}

// SetGainAuto and SetGainManual switch hardware-AGC vs. manual control
// for LNA and mixer together. Automatic: LNA mode bit (0x05 bit4) = 0,
// mixer mode bit (0x07 bit4) = 1 -- opposite polarity between the two
// fields.
func (tu *Tuner) SetGainAuto() error {
	return tu.setGainMode(0, 1)
}

func (tu *Tuner) SetGainManual() error {
	return tu.setGainMode(1, 0)
}

func (tu *Tuner) setGainMode(lna, mix byte) error {
	if err := tu.setField(0x05, 0x10, 4, lna); err != nil {
		return err
	}
	return tu.setField(0x07, 0x10, 4, mix)
}

// SetLNAGain sets the manual LNA gain code (0-15).
func (tu *Tuner) SetLNAGain(code int) error {
	return tu.setField(0x05, 0x0f, 0, byte(code))
}

// SetMixerGain sets the manual mixer gain code (0-15).
func (tu *Tuner) SetMixerGain(code int) error {
	return tu.setField(0x07, 0x0f, 0, byte(code))
}

// GainReadback reads the chip's current mixer/LNA gain codes
// (register 0x03).
func (tu *Tuner) GainReadback() (mixer, lna int, err error) {
	v, err := tu.readStatus(0x03)
	if err != nil {
		return 0, 0, err
	}
	return int(v >> 4 & 0xf), int(v & 0xf), nil
}

// compositeSearch walks the LNA and mixer step tables alternately
// (interleaved, not "max LNA first"), accumulating relative units,
// stopping once the running total meets or exceeds target. Preserved
// to match existing gain-selection behavior (RFC §10.6); a
// from-scratch design MAY use any monotonic allocation instead.
func compositeSearch(target int) (lnaCode, mixerCode int) {
	total := 0
	for step := 1; step < 16; step++ {
		if total >= target {
			break
		}
		total += lnaSteps[step] - lnaSteps[step-1]
		lnaCode = step
		if total >= target {
			break
		}
		total += mixerSteps[step] - mixerSteps[step-1]
		mixerCode = step
	}
	return lnaCode, mixerCode
}

// SetGain applies one overall RF gain value (tenths of dB, from
// GainSteps) via the composite LNA+mixer search, switching both
// stages to manual mode. VGA is unaffected.
func (tu *Tuner) SetGain(tenthsDB int) error {
	if err := tu.SetGainManual(); err != nil {
		return err
	}
	lna, mixer := compositeSearch(tenthsDB)
	if err := tu.SetLNAGain(lna); err != nil {
		return err
	}
	return tu.SetMixerGain(mixer)
}

// SetExtendedGain is the R828D-specific 3-way gain control: sets LNA,
// mixer, and VGA stages independently by 0-15 index each, bypassing
// the composite search.
func (tu *Tuner) SetExtendedGain(lna, mixer, vga int) error {
	if err := tu.SetGainManual(); err != nil {
		return err
	}
	if err := tu.SetLNAGain(lna); err != nil {
		return err
	}
	if err := tu.SetMixerGain(mixer); err != nil {
		return err
	}
	return tu.SetVGAGain(vga)
}

// SetVGAGain sets the manual IF/VGA gain code directly (0-15, RFC
// Table 15) -- the "Set IF gain (staged)" operation of Table 7. On a
// Blog V4 board this is overridden unconditionally; see blogv4.go.
func (tu *Tuner) SetVGAGain(code int) error {
	if tu.blogV4 {
		code = blogV4FixedVGACode
	}
	return tu.setField(0x0c, 0x0f, 0, byte(code))
}

const defaultVGACode = 11

// SetIFMode is the "Set IF mode" host-API convenience operation
// (Table 7): a single integer selects meaning by range against the
// VGA relative-unit scale (RFC §10.6, "IF-gain request encoding"):
//
//	0 or 10016            -> default (code 11)
//	[-2500, 2500]         -> nearest VGA step by relative dB*100 units
//	(2500, 10000)         -> same, after subtracting 5000 first
//	[10000, 10015]         -> raw code (value - 10000)
//	anything else          -> default
func (tu *Tuner) SetIFMode(value int) error {
	code := defaultVGACode
	switch {
	case value == 0 || value == 10016:
		code = defaultVGACode
	case value >= -2500 && value <= 2500:
		code = nearestVGAStep(value)
	case value > 2500 && value < 10000:
		code = nearestVGAStep(value - 5000)
	case value >= 10000 && value <= 10015:
		code = value - 10000
	}
	return tu.SetVGAGain(code)
}

// nearestVGAStep maps a signed hundredths-of-dB value onto the
// calibrated VGA scale (-12.0 + code*3.5 dB, i.e. 350 hundredths-dB
// per step).
func nearestVGAStep(centidB int) int {
	code := (centidB + 1200 + 175) / 350 // round to nearest 350-centidB step starting at -1200
	code = max(code, 0)
	code = min(code, 15)
	return code
}
