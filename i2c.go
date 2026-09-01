package r82xx

// I2C bus pass-through, block 6 (RFC §5.3). wValue holds the I2C slave
// address in 8-bit form (7-bit address shifted left by one, R/W bit 0).

func i2cAddr8(addr7 uint8) uint16 { return uint16(addr7) << 1 }

// i2cWrite performs a burst write: register offset followed by data,
// one I2C transaction (register address auto-increments on the wire).
func i2cWrite(t Transport, addr7, reg uint8, data []byte) error {
	buf := make([]byte, 1+len(data))
	buf[0] = reg
	copy(buf[1:], data)
	return blockWrite(t, blockI2C, i2cAddr8(addr7), buf)
}

// i2cRead performs a register read as two back-to-back USB
// transactions: an OUT write of the 1-byte register offset, then a
// separate IN read of the requested byte count. This is a
// write-then-read, not a true I2C repeated start; the bridge holds the
// bus between the two transactions.
func i2cRead(t Transport, addr7, reg uint8, n int) ([]byte, error) {
	if err := blockWrite(t, blockI2C, i2cAddr8(addr7), []byte{reg}); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if err := blockRead(t, blockI2C, i2cAddr8(addr7), buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// The I2C repeater is a gateway inside the demodulator that must be
// enabled before the tuner's I2C bus is reachable at all. It reuses
// the demodulator soft-reset register (page 1, address 0x01): 0x18
// enables the repeater (reset released), 0x10 disables it (RFC §5.3,
// §7.1 step 6). Repeater state is shared, global, chip-wide state, so
// callers MUST serialize concurrent tuner-facing I2C transactions
// (Device.mu does this for every exported method).

func repeaterEnable(t Transport) error  { return demodWriteReg(t, 1, 0x01, 0x18, 1) }
func repeaterDisable(t Transport) error { return demodWriteReg(t, 1, 0x01, 0x10, 1) }

func withRepeater(t Transport, fn func() error) error {
	if err := repeaterEnable(t); err != nil {
		return err
	}
	err := fn()
	if derr := repeaterDisable(t); derr != nil && err == nil {
		err = derr
	}
	return err
}
