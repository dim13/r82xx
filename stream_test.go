package r82xx

import (
	"sync/atomic"
	"testing"
	"time"
)

// fakeTransport is a minimal in-memory Transport: it always succeeds
// and echoes zero bytes on read.
type fakeTransport struct{}

func (f *fakeTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	return nil
}

func (f *fakeTransport) BulkIn(data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) Strings() (string, string, error) {
	return "", "", nil
}

func TestReadAsyncStop(t *testing.T) {
	d := &Device{t: &fakeTransport{}}
	var n atomic.Int64
	stop := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		close(stop)
	}()
	err := d.ReadAsync(func(data []byte) { n.Add(1) }, 4, 512, stop)
	if err != nil {
		t.Fatalf("ReadAsync: %v", err)
	}
	if n.Load() == 0 {
		t.Error("ReadAsync: callback never ran")
	}
}

func TestReadAsyncDeviceLost(t *testing.T) {
	d := &Device{t: &failingTransport{}}
	err := d.ReadAsync(func([]byte) {}, 3, 512, nil)
	if err != ErrDeviceLost {
		t.Errorf("ReadAsync err = %v, want ErrDeviceLost", err)
	}
}

type failingTransport struct{ fakeTransport }

func (failingTransport) BulkIn(data []byte) (int, error) {
	return 0, errAlwaysFails
}

var errAlwaysFails = errBulk{}

type errBulk struct{}

func (errBulk) Error() string {
	return "bulk transfer failed"
}
