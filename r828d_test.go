package r82xx

import "testing"

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
	tu := &Tuner{ifCenterHz: 3_570_000, sidebandUpper: false}
	// Fundamental: LO sits exactly one IF center away from the
	// requested frequency, and is unprogrammed (program == lo).
	lo, program := tu.loTargets(100_000_000, 1)
	if diff := lo - 100_000_000; diff != 3_570_000 && diff != -3_570_000 {
		t.Errorf("loTargets fundamental lo = %v, want %v +/- IF center", lo, 100_000_000.0)
	}
	if program != lo {
		t.Errorf("loTargets fundamental program = %v, want %v (== lo)", program, lo)
	}
	// Toggling sideband flips which side of the request the LO lands on.
	tu.sidebandUpper = true
	lo2, _ := tu.loTargets(100_000_000, 1)
	if lo2 == lo {
		t.Error("loTargets: toggling sidebandUpper should change the LO sign")
	}
	// Harmonic order 5: program is divided, lo (for band table) is not.
	lo5, program5 := tu.loTargets(2_000_000_000, 5)
	if program5 != lo5/5 {
		t.Errorf("loTargets harmonic program = %v, want lo/5 = %v", program5, lo5/5)
	}
}
