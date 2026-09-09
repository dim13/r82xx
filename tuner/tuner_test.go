package tuner

import "testing"

// fakeTransport is a minimal in-memory Transport for table-driven
// tests: it always succeeds, echoes zero bytes on read, and records
// every ControlOut call for assertions.
type fakeTransport struct {
	controlOuts []struct{ wValue, wIndex uint16 }
}

func (f *fakeTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	f.controlOuts = append(f.controlOuts, struct{ wValue, wIndex uint16 }{wValue, wIndex})
	return nil
}

func (f *fakeTransport) BulkIn(data []byte) (int, error) {
	return len(data), nil
}

func (f *fakeTransport) Strings() (string, string, error) {
	return "", "", nil
}

func TestNewTunerParametrization(t *testing.T) {
	r820t := New(&fakeTransport{}, R820T, R820TNominalCrystalHz, true /* forced off */)
	if r820t.blogV4 {
		t.Error("New(R820T, blogV4=true) should force blogV4 off")
	}
	if r820t.addr7 != r820tAddr7 || r820t.vcoTarget != 2 {
		t.Errorf("R820T tuner addr7=%#x vcoTarget=%d, want %#x, 2", r820t.addr7, r820t.vcoTarget, r820tAddr7)
	}

	r828d := New(&fakeTransport{}, R828D, R828DNominalCrystalHz, false)
	if r828d.addr7 != r828dAddr7 || r828d.vcoTarget != 1 {
		t.Errorf("R828D tuner addr7=%#x vcoTarget=%d, want %#x, 1", r828d.addr7, r828d.vcoTarget, r828dAddr7)
	}
}

func TestChooseDivider(t *testing.T) {
	if idx, ok := chooseDivider(900_000_000); !ok || idx != 0 {
		t.Errorf("chooseDivider(900MHz) = %d,%v, want 0,true", idx, ok)
	}
	if idx, ok := chooseDivider(100_000_000); !ok {
		t.Fatalf("chooseDivider(100MHz) failed")
	} else if vco := 100_000_000 * float64(int(1)<<(idx+1)); vco < vcoRange[0] || vco > vcoRange[1] {
		t.Errorf("chooseDivider(100MHz) idx=%d gives out-of-range VCO %v", idx, vco)
	}
	if _, ok := chooseDivider(50_000_000_000); ok {
		t.Errorf("chooseDivider(50GHz) should fail (unreachable)")
	}
}

func TestReverseByte(t *testing.T) {
	cases := []struct{ in, want byte }{
		{0x00, 0x00},
		{0xff, 0xff},
		{0x01, 0x80},
		{0x80, 0x01},
		{0x69, 0x96},
	}
	for _, tc := range cases {
		if got := reverseByte(tc.in); got != tc.want {
			t.Errorf("reverseByte(%#x) = %#x, want %#x", tc.in, got, tc.want)
		}
	}
}

func TestLookupBand(t *testing.T) {
	cases := []struct {
		mhz            float64
		wantCornerByte byte
	}{
		{0, 0xdf},
		{49, 0xdf},
		{50, 0xbe},
		{279, 0x11},
		{280, 0x00},
		{309, 0x00},
		{310, 0x00},
		{650, 0x00},
		{700, 0x00}, // beyond last row: sticks to the last row
	}
	for _, tc := range cases {
		row := lookupBand(tc.mhz * 1e6)
		if row.cornerByte != tc.wantCornerByte {
			t.Errorf("lookupBand(%vMHz).cornerByte = %#x, want %#x", tc.mhz, row.cornerByte, tc.wantCornerByte)
		}
	}
	// Bypass/engaged and band-select transition points from Table 10.
	if row := lookupBand(280e6); row.trackBypass {
		t.Error("280MHz should still be tracking-engaged")
	}
	if row := lookupBand(310e6); !row.trackBypass || row.trackBand != 1 {
		t.Errorf("310MHz should be bypass/mid band, got bypass=%v band=%d", row.trackBypass, row.trackBand)
	}
	if row := lookupBand(588e6); !row.trackBypass || row.trackBand != 0 {
		t.Errorf("588MHz should be bypass/highest band, got bypass=%v band=%d", row.trackBypass, row.trackBand)
	}
}

func TestNearestNarrowRow(t *testing.T) {
	cases := []struct {
		hz          float64
		wantNominal float64
	}{
		{290_000, 290_000},
		{0, 290_000},       // below first row: clamps to first
		{330_000, 290_000}, // midpoint(290k,375k)=332.5k
		{340_000, 375_000},
		{5_000_000, 5_000_000},
		{10_000_000, 5_000_000}, // above last row: clamps to last
	}
	for _, tc := range cases {
		if got := nearestNarrowRow(tc.hz).nominalHz; got != tc.wantNominal {
			t.Errorf("nearestNarrowRow(%v).nominalHz = %v, want %v", tc.hz, got, tc.wantNominal)
		}
	}
}

func TestCompositeSearch(t *testing.T) {
	// A target of 0 should pick the bottom of both tables.
	if lna, mixer := compositeSearch(0); lna != 0 || mixer != 0 {
		t.Errorf("compositeSearch(0) = %d,%d, want 0,0", lna, mixer)
	}
	// A very large target should saturate at the top of both tables.
	if lna, mixer := compositeSearch(1_000_000); lna != 15 || mixer != 15 {
		t.Errorf("compositeSearch(huge) = %d,%d, want 15,15", lna, mixer)
	}
}

func TestNearestVGAStep(t *testing.T) {
	cases := []struct {
		centidB  int
		wantCode int
	}{
		{-1200, 0},
		{0, 3}, // -1.5dB is code3, 2.0dB is code4; 0 is nearer -1.5 (step 350 boundary math)
		{4050, 15},
		{-5000, 0},
		{10000, 15},
	}
	for _, tc := range cases {
		if got := nearestVGAStep(tc.centidB); got != tc.wantCode {
			t.Errorf("nearestVGAStep(%d) = %d, want %d", tc.centidB, got, tc.wantCode)
		}
	}
}

func TestLOTargets(t *testing.T) {
	tu := &Tuner{
		ifCenterHz:    3_570_000,
		SidebandUpper: false,
	}
	// Fundamental: LO sits exactly one IF center away from the
	// requested frequency, and is unprogrammed (program == lo).
	lo, program := tu.LOTargets(100_000_000, 1)
	if diff := lo - 100_000_000; diff != 3_570_000 && diff != -3_570_000 {
		t.Errorf("LOTargets fundamental lo = %v, want %v +/- IF center", lo, 100_000_000.0)
	}
	if program != lo {
		t.Errorf("LOTargets fundamental program = %v, want %v (== lo)", program, lo)
	}
	// Toggling sideband flips which side of the request the LO lands on.
	tu.SidebandUpper = true
	lo2, _ := tu.LOTargets(100_000_000, 1)
	if lo2 == lo {
		t.Error("LOTargets: toggling SidebandUpper should change the LO sign")
	}
	// Harmonic order 5: program is divided, lo (for band table) is not.
	lo5, program5 := tu.LOTargets(2_000_000_000, 5)
	if program5 != lo5/5 {
		t.Errorf("LOTargets harmonic program = %v, want lo/5 = %v", program5, lo5/5)
	}
}

func TestUpconvertHF(t *testing.T) {
	cases := []struct {
		requestedHz float64
		wantHz      float64
	}{
		{1_000_000, 29_800_000},
		{28_800_000, 28_800_000}, // boundary: not shifted
		{28_799_999, 57_599_999},
		{100_000_000, 100_000_000},
	}
	for _, tc := range cases {
		if got := UpconvertHF(tc.requestedHz); got != tc.wantHz {
			t.Errorf("UpconvertHF(%v) = %v, want %v", tc.requestedHz, got, tc.wantHz)
		}
	}
}

func TestNotchEnabled(t *testing.T) {
	cases := []struct {
		hz   float64
		want bool
	}{
		{1_000_000, false},
		{2_200_000, false},
		{2_200_001, true},
		{100_000_000, false},
		{113_000_000, true},
		{200_000_000, false},
		{300_000_000, true},
	}
	for _, tc := range cases {
		if got := notchEnabled(tc.hz); got != tc.want {
			t.Errorf("notchEnabled(%v) = %v, want %v", tc.hz, got, tc.want)
		}
	}
}

func TestSelectInputBand(t *testing.T) {
	cases := []struct {
		hz   float64
		want inputBand
	}{
		{28_800_000, inputHF},
		{28_800_001, inputVHF},
		{249_999_999, inputVHF},
		{250_000_000, inputUHF},
	}
	for _, tc := range cases {
		if got := selectInputBand(tc.hz); got != tc.want {
			t.Errorf("selectInputBand(%v) = %v, want %v", tc.hz, got, tc.want)
		}
	}
}

func TestApplyInputSwitchCaching(t *testing.T) {
	ft := &fakeTransport{}
	tu := New(ft, R828D, BlogV4CrystalHz, true)
	if err := tu.ApplyInputSwitch(1_000_000); err != nil {
		t.Fatal(err)
	}
	n1 := len(ft.controlOuts)
	if err := tu.ApplyInputSwitch(2_000_000); err != nil { // still HF band
		t.Fatal(err)
	}
	if len(ft.controlOuts) != n1 {
		t.Errorf("ApplyInputSwitch issued I2C traffic for a same-band retune")
	}
	if err := tu.ApplyInputSwitch(300_000_000); err != nil { // now UHF band
		t.Fatal(err)
	}
	if len(ft.controlOuts) == n1 {
		t.Errorf("ApplyInputSwitch skipped I2C traffic on a band change")
	}
}

func TestNominalCrystalHz(t *testing.T) {
	cases := []struct {
		chip   Type
		blogV4 bool
		want   float64
	}{
		{R820T, false, R820TNominalCrystalHz},
		{R820T, true, R820TNominalCrystalHz}, // blogV4 never true for R820T in practice
		{R828D, false, R828DNominalCrystalHz},
		{R828D, true, BlogV4CrystalHz},
	}
	for _, tc := range cases {
		if got := NominalCrystalHz(tc.chip, tc.blogV4); got != tc.want {
			t.Errorf("NominalCrystalHz(%v, %v) = %v, want %v", tc.chip, tc.blogV4, got, tc.want)
		}
	}
}
