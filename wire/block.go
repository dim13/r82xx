package wire

// Block selects the internal address space a generic register access
// targets (RFC Table 2). Only the blocks accessed from outside this
// package (Sys, USB, I2C) are exported constants.
type Block uint16

const (
	blockDemod Block = 0 // demodulator, legacy flat access; superseded by demod's paged scheme
	USB        Block = 1 // USB core
	Sys        Block = 2 // system / GPIO
	blockTuner Block = 3 // tuner; unused directly, I2C block used instead
	blockROM   Block = 4 // EEPROM
	blockIR    Block = 5 // infrared; aliased into the system block's address space
	I2C        Block = 6 // I2C bus
)

// readIndex and writeIndex return the wIndex for a generic block
// access. The infrared block is not addressed directly: it is wired
// up as a sub-page of the system block (RFC §5.2).
func (b Block) readIndex() uint16 {
	if b == blockIR {
		return uint16(Sys)<<8 | 0x01
	}
	return uint16(b) << 8
}

func (b Block) writeIndex() uint16 {
	if b == blockIR {
		return uint16(Sys)<<8 | 0x11
	}
	return uint16(b)<<8 | 0x10
}

// Read performs a raw block read of len(data) bytes at wValue.
func (b Block) Read(t Transport, wValue uint16, data []byte) error {
	_, err := t.ControlIn(wValue, b.readIndex(), data)
	return err
}

// Write performs a raw block write of data at wValue.
func (b Block) Write(t Transport, wValue uint16, data []byte) error {
	return t.ControlOut(wValue, b.writeIndex(), data)
}

// ReadReg reads an n-byte register at addr within b. A 2-byte read
// reassembles little-endian, asymmetric with the big-endian write
// below (RFC §5.2; unverified, see §15 item 1).
func (b Block) ReadReg(t Transport, addr uint16, n int) (uint16, error) {
	buf := make([]byte, n)
	if err := b.Read(t, addr, buf); err != nil {
		return 0, err
	}
	if n == 1 {
		return uint16(buf[0]), nil
	}
	return uint16(buf[0]) | uint16(buf[1])<<8, nil
}

// WriteReg writes an n-byte register at addr within b.
func (b Block) WriteReg(t Transport, addr uint16, val uint16, n int) error {
	buf := make([]byte, n)
	if n == 1 {
		buf[0] = byte(val)
	} else {
		buf[0], buf[1] = byte(val>>8), byte(val)
	}
	return b.Write(t, addr, buf)
}
