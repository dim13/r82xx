// Package i2c implements the I2C bus pass-through, block 6 (RFC
// §5.3). wValue holds the I2C slave address in 8-bit form (7-bit
// address shifted left by one, R/W bit 0).
package i2c

import "github.com/dim13/r82xx/wire"

func addr8(addr7 uint8) uint16 {
	return uint16(addr7) << 1
}

// Write performs a burst write: register offset followed by data, one
// I2C transaction (register address auto-increments on the wire).
func Write(t wire.Transport, addr7, reg uint8, data []byte) error {
	buf := make([]byte, 1+len(data))
	buf[0] = reg
	copy(buf[1:], data)
	return wire.I2C.Write(t, addr8(addr7), buf)
}

// Read performs a register read as two back-to-back USB transactions:
// an OUT write of the 1-byte register offset, then a separate IN read
// of the requested byte count. This is a write-then-read, not a true
// I2C repeated start; the bridge holds the bus between the two
// transactions.
func Read(t wire.Transport, addr7, reg uint8, n int) ([]byte, error) {
	if err := wire.I2C.Write(t, addr8(addr7), []byte{reg}); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if err := wire.I2C.Read(t, addr8(addr7), buf); err != nil {
		return nil, err
	}
	return buf, nil
}
