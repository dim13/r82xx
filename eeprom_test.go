package r82xx

import "testing"

func TestLayoutRoundTrip(t *testing.T) {
	want := Layout{
		VendorID:     VendorID,
		ProductID:    ProductID,
		SerialEnable: true,
		RemoteWakeup: true,
		Manufacturer: "RTLSDRBlog",
		Product:      "Blog V4",
		Serial:       "00000001",
	}
	b, err := EncodeLayout(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 79 {
		t.Fatalf("EncodeLayout produced %d bytes, want 79", len(b))
	}
	got, err := DecodeLayout(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestDecodeLayoutBadMagic(t *testing.T) {
	b := make([]byte, 79)
	if _, err := DecodeLayout(b); err == nil {
		t.Error("DecodeLayout accepted a buffer with bad header magic")
	}
}

func TestEncodeLayoutTruncation(t *testing.T) {
	long := Layout{Manufacturer: "this manufacturer string is deliberately far too long to fit"}
	if _, err := EncodeLayout(long); err != ErrEEPROMTruncated {
		t.Errorf("EncodeLayout(long) err = %v, want ErrEEPROMTruncated", err)
	}
}
