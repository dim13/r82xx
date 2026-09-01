package r82xx

// GPIORegs names the four one-byte system-block GPIO registers (RFC
// §7.9). The RFC gives no concrete addresses for them (Table 5 lists
// roles only) -- guessing would misprogram real hardware, so the
// caller supplies them (e.g. off a datasheet or logic-analyzer capture).
type GPIORegs struct {
	Input, Output, OutputEnable, PullDisable uint8
}

func gpioBit(pin int) uint16 { return 1 << uint(pin) }

// ReadPin reads one GPIO input line's state.
func ReadPin(t Transport, r GPIORegs, pin int) (bool, error) {
	v, err := readReg(t, blockSys, uint16(r.Input), 1)
	if err != nil {
		return false, err
	}
	return v&gpioBit(pin) != 0, nil
}

// WritePin drives one GPIO output line via read-modify-write.
func WritePin(t Transport, r GPIORegs, pin int, high bool) error {
	v, err := readReg(t, blockSys, uint16(r.Output), 1)
	if err != nil {
		return err
	}
	if high {
		v |= gpioBit(pin)
	} else {
		v &^= gpioBit(pin)
	}
	return writeReg(t, blockSys, uint16(r.Output), v, 1)
}

// ConfigurePin sets one line's direction: clearing its pull/disable
// bit and setting its output-enable bit configures it as output;
// setting pull/disable and clearing output-enable configures it as
// input. (Unverified: which register is direction vs. pull/disable is
// inferred from usage, not confirmed -- RFC §15 item 3.)
func ConfigurePin(t Transport, r GPIORegs, pin int, output bool) error {
	pd, err := readReg(t, blockSys, uint16(r.PullDisable), 1)
	if err != nil {
		return err
	}
	oe, err := readReg(t, blockSys, uint16(r.OutputEnable), 1)
	if err != nil {
		return err
	}
	if output {
		pd &^= gpioBit(pin)
		oe |= gpioBit(pin)
	} else {
		pd |= gpioBit(pin)
		oe &^= gpioBit(pin)
	}
	if err := writeReg(t, blockSys, uint16(r.PullDisable), pd, 1); err != nil {
		return err
	}
	return writeReg(t, blockSys, uint16(r.OutputEnable), oe, 1)
}

// PulsePin drives a line high then low: a hardware reset strobe for
// one tuner-family candidate during detection when nothing has
// matched yet (RFC §7.9, §8) -- moot for R820T/R828D, which are
// detected before this fallback fires.
func PulsePin(t Transport, r GPIORegs, pin int) error {
	if err := WritePin(t, r, pin, true); err != nil {
		return err
	}
	return WritePin(t, r, pin, false)
}
