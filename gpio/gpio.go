// Package gpio implements the system-block GPIO registers (RFC §7.9).
package gpio

import (
	"github.com/dim13/r82xx/wire"
)

// Regs names the four one-byte system-block GPIO registers. The RFC
// gives no concrete addresses for them (Table 5 lists roles only) --
// guessing would misprogram real hardware, so the caller supplies them
// (e.g. off a datasheet or logic-analyzer capture).
type Regs struct {
	Input        uint8
	Output       uint8
	OutputEnable uint8
	PullDisable  uint8
}

func bit(pin int) uint16 {
	return 1 << uint(pin)
}

// ReadPin reads one GPIO input line's state.
func ReadPin(t wire.Transport, r Regs, pin int) (bool, error) {
	v, err := wire.BlockSys.ReadReg(t, uint16(r.Input), 1)
	if err != nil {
		return false, err
	}
	return v&bit(pin) != 0, nil
}

// WritePin drives one GPIO output line via read-modify-write.
func WritePin(t wire.Transport, r Regs, pin int, high bool) error {
	v, err := wire.BlockSys.ReadReg(t, uint16(r.Output), 1)
	if err != nil {
		return err
	}
	if high {
		v |= bit(pin)
	} else {
		v &^= bit(pin)
	}
	return wire.BlockSys.WriteReg(t, uint16(r.Output), v, 1)
}

// ConfigurePin sets one line's direction: clearing its pull/disable
// bit and setting its output-enable bit configures it as output;
// setting pull/disable and clearing output-enable configures it as
// input. (Unverified: which register is direction vs. pull/disable is
// inferred from usage, not confirmed -- RFC §15 item 3.)
func ConfigurePin(t wire.Transport, r Regs, pin int, output bool) error {
	pd, err := wire.BlockSys.ReadReg(t, uint16(r.PullDisable), 1)
	if err != nil {
		return err
	}
	oe, err := wire.BlockSys.ReadReg(t, uint16(r.OutputEnable), 1)
	if err != nil {
		return err
	}
	if output {
		pd &^= bit(pin)
		oe |= bit(pin)
	} else {
		pd |= bit(pin)
		oe &^= bit(pin)
	}
	if err := wire.BlockSys.WriteReg(t, uint16(r.PullDisable), pd, 1); err != nil {
		return err
	}
	return wire.BlockSys.WriteReg(t, uint16(r.OutputEnable), oe, 1)
}

// PulsePin drives a line high then low: a hardware reset strobe for
// one tuner-family candidate during detection when nothing has
// matched yet (RFC §7.9, §8) -- moot for R820T/R828D, which are
// detected before this fallback fires.
func PulsePin(t wire.Transport, r Regs, pin int) error {
	if err := WritePin(t, r, pin, true); err != nil {
		return err
	}
	return WritePin(t, r, pin, false)
}
