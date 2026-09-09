// Package wire implements the USB control-transfer wire protocol
// shared by every higher-level register access in this module: block
// selection and generic block/register read/write.
package wire

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
