// Package r82xx implements the RTL2832U + R828D USB SDR receiver as
// fitted to the RTL-SDR Blog V4, derived from docs/hardware-spec-rfc.txt.
package r82xx

// USB device identity (RFC §4).
const (
	VendorID  = 0x0bda
	ProductID = 0x2838
)

// Transport is the USB control- and bulk-transfer primitive a caller
// supplies; this package contains no USB backend of its own.
//
// Every non-streaming access is a USB vendor-class control transfer
// (bmRequestType vendor, bRequest 0) with a 300ms timeout; a Transport
// implementation MUST apply that timeout itself. BulkIn services the
// sample endpoint and SHOULD apply a 10s timeout in synchronous use.
type Transport interface {
	ControlIn(wValue, wIndex uint16, data []byte) (int, error)
	ControlOut(wValue, wIndex uint16, data []byte) error
	BulkIn(data []byte) (int, error)

	// Strings returns the standard USB manufacturer and product string
	// descriptors, used for board identification (RFC §4, §8).
	Strings() (manufacturer, product string, err error)
}

// block selects the internal address space a generic register access
// targets (RFC Table 2).
type block uint16

const (
	blockDemod block = 0
	blockUSB   block = 1
	blockSys   block = 2
	blockTuner block = 3
	blockROM   block = 4
	blockIR    block = 5
	blockI2C   block = 6
)

// blockIndex returns the wIndex for a generic block access. The
// infrared block is not addressed directly: it is wired up as a
// sub-page of the system block (RFC §5.2).
func blockIndex(b block, write bool) uint16 {
	if b == blockIR {
		if write {
			return uint16(blockSys)<<8 | 0x11
		}
		return uint16(blockSys)<<8 | 0x01
	}
	w := uint16(b) << 8
	if write {
		w |= 0x10
	}
	return w
}

func blockRead(t Transport, b block, wValue uint16, data []byte) error {
	_, err := t.ControlIn(wValue, blockIndex(b, false), data)
	return err
}

func blockWrite(t Transport, b block, wValue uint16, data []byte) error {
	return t.ControlOut(wValue, blockIndex(b, true), data)
}

// A 2-byte read reassembles little-endian, asymmetric with the
// big-endian write below (RFC §5.2; unverified, see §15 item 1).
func readReg(t Transport, b block, addr uint16, n int) (uint16, error) {
	buf := make([]byte, n)
	if err := blockRead(t, b, addr, buf); err != nil {
		return 0, err
	}
	if n == 1 {
		return uint16(buf[0]), nil
	}
	return uint16(buf[0]) | uint16(buf[1])<<8, nil
}

func writeReg(t Transport, b block, addr uint16, val uint16, n int) error {
	buf := make([]byte, n)
	if n == 1 {
		buf[0] = byte(val)
	} else {
		buf[0], buf[1] = byte(val>>8), byte(val)
	}
	return blockWrite(t, b, addr, buf)
}
