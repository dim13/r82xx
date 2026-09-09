package wire

// Read performs a raw block read at wValue within b.
func Read(t Transport, b Block, wValue uint16, data []byte) error {
	_, err := t.ControlIn(wValue, b.readIndex(), data)
	return err
}

// Write performs a raw block write at wValue within b.
func Write(t Transport, b Block, wValue uint16, data []byte) error {
	return t.ControlOut(wValue, b.writeIndex(), data)
}

// ReadReg reassembles a 2-byte read little-endian, asymmetric with the
// big-endian write below (RFC §5.2; unverified, see §15 item 1).
func ReadReg(t Transport, b Block, addr uint16, n int) (uint16, error) {
	buf := make([]byte, n)
	if err := Read(t, b, addr, buf); err != nil {
		return 0, err
	}
	if n == 1 {
		return uint16(buf[0]), nil
	}
	return uint16(buf[0]) | uint16(buf[1])<<8, nil
}

// WriteReg writes an n-byte register at addr within b.
func WriteReg(t Transport, b Block, addr uint16, val uint16, n int) error {
	buf := make([]byte, n)
	if n == 1 {
		buf[0] = byte(val)
	} else {
		buf[0], buf[1] = byte(val>>8), byte(val)
	}
	return Write(t, b, addr, buf)
}
