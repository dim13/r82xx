package r82xx

import "testing"

type demodWrite struct {
	page, addr uint8
	val        byte
}

// fakeTransport is a minimal in-memory Transport for table-driven
// tests: it always succeeds, echoes zero bytes on read, and records
// every call for assertions.
type fakeTransport struct {
	manufacturer, product string

	controlOuts []struct{ wValue, wIndex uint16 }
	demodWrites []demodWrite
}

func (f *fakeTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	f.controlOuts = append(f.controlOuts, struct{ wValue, wIndex uint16 }{wValue, wIndex})
	if wValue&0xff == 0x20 && wIndex&0x10 != 0 && len(data) == 1 {
		f.demodWrites = append(f.demodWrites, demodWrite{
			page: uint8(wIndex & 0x0f),
			addr: uint8(wValue >> 8),
			val:  data[0],
		})
	}
	return nil
}

func (f *fakeTransport) BulkIn(data []byte) (int, error) { return len(data), nil }

func (f *fakeTransport) Strings() (string, string, error) {
	return f.manufacturer, f.product, nil
}

func TestBlockIndex(t *testing.T) {
	cases := []struct {
		b     block
		write bool
		want  uint16
	}{
		{blockDemod, false, 0x0000},
		{blockDemod, true, 0x0010},
		{blockI2C, false, 0x0600},
		{blockI2C, true, 0x0610},
		{blockIR, false, 0x0201}, // aliased into the system block
		{blockIR, true, 0x0211},
	}
	for _, tc := range cases {
		if got := blockIndex(tc.b, tc.write); got != tc.want {
			t.Errorf("blockIndex(%v, %v) = %#x, want %#x", tc.b, tc.write, got, tc.want)
		}
	}
}

func TestReadWriteRegByteOrder(t *testing.T) {
	// 2-byte write is big-endian; 2-byte read is reassembled
	// little-endian (RFC §5.2, unverified asymmetry).
	ft := &fakeTransport{}
	if err := writeReg(ft, blockUSB, 0x00, 0x1234, 2); err != nil {
		t.Fatal(err)
	}

	var sent []byte
	sentTransport := &recordingTransport{}
	if err := writeReg(sentTransport, blockUSB, 0x00, 0x1234, 2); err != nil {
		t.Fatal(err)
	}
	sent = sentTransport.lastOut
	if want := []byte{0x12, 0x34}; sent[0] != want[0] || sent[1] != want[1] {
		t.Errorf("write bytes = %#x, want %#x (big-endian)", sent, want)
	}

	readTransport := &recordingTransport{inData: []byte{0x34, 0x12}}
	got, err := readReg(readTransport, blockUSB, 0x00, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := uint16(0x1234); got != want {
		t.Errorf("readReg = %#x, want %#x (little-endian reassembly)", got, want)
	}
}

type recordingTransport struct {
	lastOut []byte
	inData  []byte
}

func (r *recordingTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	n := copy(data, r.inData)
	return n, nil
}
func (r *recordingTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	r.lastOut = append([]byte(nil), data...)
	return nil
}
func (r *recordingTransport) BulkIn(data []byte) (int, error)  { return 0, nil }
func (r *recordingTransport) Strings() (string, string, error) { return "", "", nil }

func TestI2CAddr8(t *testing.T) {
	if got, want := i2cAddr8(0x3a), uint16(0x74); got != want {
		t.Errorf("i2cAddr8(0x3a) = %#x, want %#x", got, want)
	}
}
