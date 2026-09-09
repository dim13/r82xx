package demod

import "github.com/dim13/r82xx/wire"

// Demodulator paged-register access (RFC §5.4): wValue = (addr<<8)|0x20,
// wIndex = page (read) or 0x10|page (write). Every write MUST be
// followed by a fixed dummy read (page 0x0A, address 0x01) as a
// commit/flush step.

func demodReadReg(t wire.Transport, page, addr uint8, n int) (uint16, error) {
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

func demodWriteReg(t wire.Transport, page, addr uint8, val uint16, n int) error {
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

// The I2C repeater is a gateway inside the demodulator that must be
// enabled before the tuner's I2C bus is reachable at all. It reuses
// the demodulator soft-reset register (page 1, address 0x01): 0x18
// enables the repeater (reset released), 0x10 disables it (RFC §5.3,
// §7.1 step 6). Repeater state is shared, global, chip-wide state, so
// callers MUST serialize concurrent tuner-facing I2C transactions
// (Device.mu does this for every exported method).

// RepeaterEnable releases the reset, opening the tuner's I2C bus.
func RepeaterEnable(t wire.Transport) error {
	return demodWriteReg(t, 1, 0x01, 0x18, 1)
}

// RepeaterDisable re-asserts the reset, closing the tuner's I2C bus.
func RepeaterDisable(t wire.Transport) error {
	return demodWriteReg(t, 1, 0x01, 0x10, 1)
}

// WithRepeater enables the repeater, runs fn, and disables it again.
func WithRepeater(t wire.Transport, fn func() error) error {
	if err := RepeaterEnable(t); err != nil {
		return err
	}
	err := fn()
	if derr := RepeaterDisable(t); derr != nil && err == nil {
		err = derr
	}
	return err
}
