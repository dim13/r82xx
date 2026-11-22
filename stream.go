package r82xx

import (
	"errors"
	"sync"

	"github.com/dim13/r82xx/wire"
)

// Bulk sample streaming (RFC §12). Samples are interleaved unsigned
// 8-bit I/Q pairs on a single bulk IN endpoint.

// Read performs one synchronous blocking bulk read into buf. Transport
// implementations SHOULD apply the 10s bulk timeout themselves.
func (d *Device) Read(buf []byte) (int, error) {
	return d.t.BulkIn(buf)
}

// ResetBuffer is a device-side FIFO flush, callable before starting a
// stream: re-arms the USB-core endpoint-control register with no
// corresponding host-side buffer-queue action.
func (d *Device) ResetBuffer() error {
	if err := wire.WriteReg(d.t, wire.BlockUSB, wire.USBEPACtl, 0x1002, 2); err != nil {
		return err
	}
	return wire.WriteReg(d.t, wire.BlockUSB, wire.USBEPACtl, 0x0000, 2)
}

const (
	defaultAsyncBuffers    = 15
	defaultAsyncBufferSize = 262_144 // 16 * 32 * 512
)

// ErrDeviceLost is returned once consecutive transfer failures reach
// the pool size (or a hard transport error occurs) -- the
// failure-tolerance threshold scales with buffer count, not a fixed
// constant (RFC §12).
var ErrDeviceLost = errors.New("r82xx: device lost (consecutive bulk transfer failures)")

// ponytail: the RFC's async pool is libusb-async-submit shaped (a
// zero-copy DMA buffer pool with kernel completion callbacks); with no
// USB backend of our own (Transport is a plain blocking primitive) we
// approximate "pool of buffers, continuously resubmitted, callback on
// completion" with one blocking BulkIn call per goroutine instead of
// true overlapped submission. Functionally equivalent for a
// synchronous Transport; upgrade to real async submit if a backend
// exposing it is ever plugged in.

// ReadAsync calls fn on every successful read across nBuffers
// concurrent readers until stop closes or the device is judged lost.
func (d *Device) ReadAsync(fn func(data []byte), nBuffers, bufSize int, stop <-chan struct{}) error {
	if nBuffers <= 0 {
		nBuffers = defaultAsyncBuffers
	}
	if bufSize <= 0 || bufSize%512 != 0 {
		bufSize = defaultAsyncBufferSize
	}

	var (
		mu       sync.Mutex
		fails    int
		lost     error
		wg       sync.WaitGroup
		doneOnce sync.Once
		done     = make(chan struct{})
	)
	stopAll := func() { doneOnce.Do(func() { close(done) }) }

	for range nBuffers {
		wg.Go(func() {
			buf := make([]byte, bufSize)
			for {
				select {
				case <-done:
					return
				case <-stop:
					stopAll()
					return
				default:
				}
				n, err := d.t.BulkIn(buf)
				mu.Lock()
				if err != nil {
					fails++
					if fails >= nBuffers {
						lost = ErrDeviceLost
						mu.Unlock()
						stopAll()
						return
					}
					mu.Unlock()
					continue
				}
				fails = 0
				mu.Unlock()
				fn(buf[:n])
			}
		})
	}
	wg.Wait()
	return lost
}
