package i2c

import "testing"

func TestAddr8(t *testing.T) {
	if got, want := addr8(0x3a), uint16(0x74); got != want {
		t.Errorf("addr8(0x3a) = %#x, want %#x", got, want)
	}
}
