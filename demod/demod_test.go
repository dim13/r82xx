package demod

import "testing"

type demodWrite struct {
	page, addr uint8
	val        byte
}

// fakeTransport is a minimal in-memory Transport for table-driven
// tests: it always succeeds, echoes zero bytes on read, and records
// every demodulator paged-register write for assertions.
type fakeTransport struct {
	demodWrites []demodWrite
}

func (f *fakeTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	if wValue&0xff == 0x20 && wIndex&0x10 != 0 && len(data) == 1 {
		f.demodWrites = append(f.demodWrites, demodWrite{
			page: uint8(wIndex & 0x0f),
			addr: uint8(wValue >> 8),
			val:  data[0],
		})
	}
	return nil
}

func (f *fakeTransport) BulkIn(data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) Strings() (string, string, error) {
	return "", "", nil
}

func TestIFRegValue(t *testing.T) {
	if got, want := ifRegValue(0, 28_800_000), int32(0); got != want {
		t.Errorf("ifRegValue(0, ...) = %#x, want %#x", got, want)
	}
	if v := ifRegValue(3_570_000, 28_800_000); v&^0x3fffff != 0 {
		t.Errorf("ifRegValue result exceeds 22 bits: %#x", v)
	}
	// Round-trip: decoding the register value back to Hz should land
	// within one LSB of the requested IF.
	got := ifRegValue(3_570_000, 28_800_000)
	// sign-extend 22-bit two's complement
	signed := got
	if signed&(1<<21) != 0 {
		signed -= 1 << 22
	}
	gotHz := -float64(signed) * 28_800_000 / (1 << 22)
	if diff := gotHz - 3_570_000; diff > 10 || diff < -10 {
		t.Errorf("round-trip IF = %v Hz, want ~3570000", gotHz)
	}
}

func TestPPMTrim(t *testing.T) {
	if got, want := ppmTrim(0), int16(0); got != want {
		t.Errorf("ppmTrim(0) = %d, want %d", got, want)
	}
	got := ppmTrim(1)
	want := int16(-17) // round(-1 * 2^24 / 1e6) = round(-16.777...)
	if got != want {
		t.Errorf("ppmTrim(1) = %d, want %d", got, want)
	}
}

func TestResamplerRatio(t *testing.T) {
	ratio, achieved := resamplerRatio(28_800_000, 2_400_000)
	if ratio&0x3 != 0 {
		t.Errorf("resamplerRatio low 2 bits not zeroed: %#x", ratio)
	}
	if ratio&0xf0000000 != 0 {
		t.Errorf("resamplerRatio exceeds 28 bits: %#x", ratio)
	}
	if diff := achieved - 2_400_000; diff > 1000 || diff < -1000 {
		t.Errorf("resamplerRatio achieved = %v, want ~2400000", achieved)
	}
}

func TestValidSampleRate(t *testing.T) {
	cases := []struct {
		hz   uint32
		want bool
	}{
		{225_000, false},
		{225_001, true},
		{300_000, true},
		{300_001, false}, // dead zone
		{900_000, false},
		{900_001, true},
		{3_200_000, true},
		{3_200_001, false},
	}
	for _, tc := range cases {
		if got := validSampleRate(tc.hz); got != tc.want {
			t.Errorf("validSampleRate(%d) = %v, want %v", tc.hz, got, tc.want)
		}
	}
}

func TestWriteFIRPacking(t *testing.T) {
	ft := &fakeTransport{}
	taps := [16]int16{
		1, -1, 2, -2, 3, -3, 4, -4, // 8-bit taps
		100, -100, 200, -200, 300, -300, 400, -400, // 12-bit taps
	}
	if err := writeFIR(ft, taps); err != nil {
		t.Fatal(err)
	}
	if got, want := len(ft.demodWrites), 20; got != want {
		t.Fatalf("wrote %d registers, want %d", got, want)
	}
	for i := range 8 {
		if got, want := ft.demodWrites[i].val, byte(int8(taps[i])); got != want {
			t.Errorf("tap %d = %#x, want %#x", i, got, want)
		}
	}
	a, b := taps[8], taps[9]
	want := [3]byte{byte(a >> 4), byte(a<<4) | byte((b>>8)&0xf), byte(b)}
	for i, w := range want {
		if got := ft.demodWrites[8+i].val; got != w {
			t.Errorf("packed byte %d = %#x, want %#x", i, got, w)
		}
	}
}
