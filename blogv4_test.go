package r82xx

import "testing"

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
	tu := NewTuner(ft, TunerR828D, blogV4CrystalHz, true)
	if err := tu.applyInputSwitch(1_000_000); err != nil {
		t.Fatal(err)
	}
	n1 := len(ft.controlOuts)
	if err := tu.applyInputSwitch(2_000_000); err != nil { // still HF band
		t.Fatal(err)
	}
	if len(ft.controlOuts) != n1 {
		t.Errorf("applyInputSwitch issued I2C traffic for a same-band retune")
	}
	if err := tu.applyInputSwitch(300_000_000); err != nil { // now UHF band
		t.Fatal(err)
	}
	if len(ft.controlOuts) == n1 {
		t.Errorf("applyInputSwitch skipped I2C traffic on a band change")
	}
}
