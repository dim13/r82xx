package wire

import "testing"

func TestBlockReadIndex(t *testing.T) {
	cases := []struct {
		b    Block
		want uint16
	}{
		{BlockDemod, 0x0000},
		{BlockI2C, 0x0600},
		{BlockIR, 0x0201}, // aliased into the system block
	}
	for _, tc := range cases {
		if got := tc.b.readIndex(); got != tc.want {
			t.Errorf("%v.readIndex() = %#x, want %#x", tc.b, got, tc.want)
		}
	}
}

func TestBlockWriteIndex(t *testing.T) {
	cases := []struct {
		b    Block
		want uint16
	}{
		{BlockDemod, 0x0010},
		{BlockI2C, 0x0610},
		{BlockIR, 0x0211}, // aliased into the system block
	}
	for _, tc := range cases {
		if got := tc.b.writeIndex(); got != tc.want {
			t.Errorf("%v.writeIndex() = %#x, want %#x", tc.b, got, tc.want)
		}
	}
}
