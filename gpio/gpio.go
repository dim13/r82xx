// Package gpio implements the system-block GPIO registers (RFC §7.9).
package gpio

import (
	"github.com/dim13/r82xx/wire"
)

func bit(pin int) uint16 {
	return 1 << uint(pin)
}

// ReadPin reads one GPIO input line's state.
func ReadPin(t wire.Transport, pin int) (bool, error) {
	v, err := wire.ReadReg(t, wire.BlockSys, wire.GPI, 1)
	if err != nil {
		return false, err
	}
	return v&bit(pin) != 0, nil
}

// WritePin drives one GPIO output line via read-modify-write.
func WritePin(t wire.Transport, pin int, high bool) error {
	v, err := wire.ReadReg(t, wire.BlockSys, wire.GPO, 1)
	if err != nil {
		return err
	}
	if high {
		v |= bit(pin)
	} else {
		v &^= bit(pin)
	}
	return wire.WriteReg(t, wire.BlockSys, wire.GPO, v, 1)
}

// ConfigurePin sets one line's direction: clearing its pull/disable
// bit and setting its output-enable bit configures it as output;
// setting pull/disable and clearing output-enable configures it as
// input. The output direction matches the reference driver on the
// wire.
func ConfigurePin(t wire.Transport, pin int, output bool) error {
	pd, err := wire.ReadReg(t, wire.BlockSys, wire.GPD, 1)
	if err != nil {
		return err
	}
	if output {
		pd &^= bit(pin)
	} else {
		pd |= bit(pin)
	}
	if err := wire.WriteReg(t, wire.BlockSys, wire.GPD, pd, 1); err != nil {
		return err
	}
	oe, err := wire.ReadReg(t, wire.BlockSys, wire.GPOE, 1)
	if err != nil {
		return err
	}
	if output {
		oe |= bit(pin)
	} else {
		oe &^= bit(pin)
	}
	return wire.WriteReg(t, wire.BlockSys, wire.GPOE, oe, 1)
}

// PulsePin drives a line high then low: a hardware reset strobe for
// one tuner-family candidate during detection when nothing has
// matched yet (RFC §7.9, §8) -- moot for R820T/R828D, which are
// detected before this fallback fires.
func PulsePin(t wire.Transport, pin int) error {
	if err := WritePin(t, pin, true); err != nil {
		return err
	}
	return WritePin(t, pin, false)
}
