package r82xx

import "testing"

// smokeTransport is a stateful fake I2C bus, just enough to carry
// Device.Open/SetFrequency/Close through a full run without a real
// RTL2832U/R828D on the other end: the R828D chip-ID register answers
// correctly, and the PLL always reports locked.
type smokeTransport struct {
	manufacturer, product string
	i2c                   map[uint8]map[uint8]byte
	pendingReg            map[uint8]uint8
}

func newSmokeTransport(manufacturer, product string) *smokeTransport {
	return &smokeTransport{
		manufacturer: manufacturer,
		product:      product,
		i2c: map[uint8]map[uint8]byte{
			0x3a: {
				0x00: chipIDVal,         // raw: detection reads this unreversed
				0x02: reverseByte(0x40), // PLL locked (bit6)
				0x04: reverseByte(0x15), // VCO op-point 1, cal code 5
			},
		},
		pendingReg: map[uint8]uint8{},
	}
}

func (s *smokeTransport) ControlIn(wValue, wIndex uint16, data []byte) (int, error) {
	if wIndex&0xff00 == uint16(blockI2C)<<8 {
		slave := uint8(wValue >> 1)
		reg := s.pendingReg[slave]
		regs := s.i2c[slave]
		for i := range data {
			data[i] = regs[reg+uint8(i)]
		}
	}
	return len(data), nil
}

func (s *smokeTransport) ControlOut(wValue, wIndex uint16, data []byte) error {
	if wIndex&0xff00 == uint16(blockI2C)<<8 {
		slave := uint8(wValue >> 1)
		if len(data) == 1 {
			s.pendingReg[slave] = data[0]
			return nil
		}
		if s.i2c[slave] == nil {
			s.i2c[slave] = map[uint8]byte{}
		}
		reg := data[0]
		for i, v := range data[1:] {
			s.i2c[slave][reg+uint8(i)] = v
		}
	}
	return nil
}

func (s *smokeTransport) BulkIn(data []byte) (int, error) {
	return len(data), nil
}

func (s *smokeTransport) Strings() (string, string, error) {
	return s.manufacturer, s.product, nil
}

func TestDeviceEndToEnd(t *testing.T) {
	tr := newSmokeTransport("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !d.blogV4 {
		t.Error("Open: expected Blog V4 board identification to succeed")
	}
	if d.tuner.crystalHz != blogV4CrystalHz {
		t.Errorf("tuner crystal = %v, want %v", d.tuner.crystalHz, blogV4CrystalHz)
	}

	if achieved, err := d.SetSampleRate(2_400_000); err != nil {
		t.Fatalf("SetSampleRate: %v", err)
	} else if diff := achieved - 2_400_000; diff > 1000 || diff < -1000 {
		t.Errorf("SetSampleRate achieved = %v, want ~2400000", achieved)
	}

	if err := d.SetFrequency(100_000_000); err != nil {
		t.Fatalf("SetFrequency: %v", err)
	}
	if !d.haveFrequency || d.lastFrequencyHz != 100_000_000 {
		t.Errorf("Device did not record the tuned frequency")
	}

	if err := d.SetGain(200); err != nil {
		t.Fatalf("SetGain: %v", err)
	}
	if err := d.SetGainMode(true); err != nil {
		t.Fatalf("SetGainMode: %v", err)
	}
	if err := d.SetExtendedGain(1, 1, 1); err != nil {
		t.Errorf("SetExtendedGain on R828D: %v", err)
	}
	if err := d.SetIFMode(0); err != nil {
		t.Errorf("SetIFMode on R828D: %v", err)
	}
	if locked, err := d.PLLLocked(); err != nil || !locked {
		t.Errorf("PLLLocked = %v, %v, want true, nil", locked, err)
	}

	// A Blog V4 board pins VGA gain unconditionally (RFC §11.5).
	if err := d.SetIFGain(15); err != nil {
		t.Fatalf("SetIFGain: %v", err)
	}
	if got := d.tuner.regs[0x0c] & 0x0f; got != blogV4FixedVGACode {
		t.Errorf("Blog V4 VGA code = %d, want pinned %d", got, blogV4FixedVGACode)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestOpenPlainR828D(t *testing.T) {
	tr := newSmokeTransport("Generic", "RTL2838UHIDIR")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if d.blogV4 {
		t.Error("Open: expected plain-board identification (no Blog V4 match)")
	}
	if d.tuner.crystalHz != r828dNominalCrystalHz {
		t.Errorf("tuner crystal = %v, want %v", d.tuner.crystalHz, float64(r828dNominalCrystalHz))
	}
}

func newSmokeTransportR820T(manufacturer, product string) *smokeTransport {
	tr := newSmokeTransport(manufacturer, product)
	delete(tr.i2c, 0x3a)
	tr.i2c[0x1a] = map[uint8]byte{
		0x00: chipIDVal,
		0x02: reverseByte(0x40), // PLL locked
		0x04: reverseByte(0x15), // VCO op-point 2 (bits5:4=01... see below), cal code 5
	}
	return tr
}

func TestOpenR820T(t *testing.T) {
	// Manufacturer/product strings deliberately match the Blog V4
	// board-id pair: Blog V4 boards only ever carry an R828D (RFC
	// §8), so an R820T match MUST NOT be treated as Blog V4 even if
	// the strings happen to match.
	tr := newSmokeTransportR820T("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if d.tuner.chip != TunerR820T {
		t.Fatalf("detected chip = %v, want TunerR820T", d.tuner.chip)
	}
	if d.blogV4 {
		t.Error("Open: R820T must never be identified as Blog V4")
	}
	if d.tuner.addr7 != r820tAddr7 {
		t.Errorf("tuner addr7 = %#x, want %#x", d.tuner.addr7, r820tAddr7)
	}
	if d.tuner.vcoTarget != 2 {
		t.Errorf("tuner vcoTarget = %d, want 2", d.tuner.vcoTarget)
	}
	if d.tuner.crystalHz != r820tNominalCrystalHz {
		t.Errorf("tuner crystal = %v, want %v", d.tuner.crystalHz, float64(r820tNominalCrystalHz))
	}

	if err := d.SetFrequency(100_000_000); err != nil {
		t.Fatalf("SetFrequency: %v", err)
	}

	// R820T-gated ops must work here...
	if err := d.SetDithering(false); err != nil {
		t.Errorf("SetDithering on R820T: %v", err)
	}
	if err := d.SetHarmonicOverride(1); err != nil {
		t.Errorf("SetHarmonicOverride on R820T: %v", err)
	}
	// ...and R828D-gated ops must not.
	if err := d.SetExtendedGain(1, 1, 1); err != ErrR828DOnly {
		t.Errorf("SetExtendedGain on R820T err = %v, want ErrR828DOnly", err)
	}
	if err := d.SetIFMode(0); err != ErrR828DOnly {
		t.Errorf("SetIFMode on R820T err = %v, want ErrR828DOnly", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestR820TGatedOpsRejectedOnR828D(t *testing.T) {
	tr := newSmokeTransport("RTLSDRBlog", "Blog V4")
	d, err := Open(tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := d.SetDithering(true); err != ErrR820TOnly {
		t.Errorf("SetDithering on R828D err = %v, want ErrR820TOnly", err)
	}
	if err := d.SetHarmonicOverride(5); err != ErrR820TOnly {
		t.Errorf("SetHarmonicOverride on R828D err = %v, want ErrR820TOnly", err)
	}
}

func TestNewTunerParametrization(t *testing.T) {
	r820t := NewTuner(&fakeTransport{}, TunerR820T, r820tNominalCrystalHz, true /* forced off */)
	if r820t.blogV4 {
		t.Error("NewTuner(TunerR820T, blogV4=true) should force blogV4 off")
	}
	if r820t.addr7 != r820tAddr7 || r820t.vcoTarget != 2 {
		t.Errorf("R820T tuner addr7=%#x vcoTarget=%d, want %#x, 2", r820t.addr7, r820t.vcoTarget, r820tAddr7)
	}

	r828d := NewTuner(&fakeTransport{}, TunerR828D, r828dNominalCrystalHz, false)
	if r828d.addr7 != r828dAddr7 || r828d.vcoTarget != 1 {
		t.Errorf("R828D tuner addr7=%#x vcoTarget=%d, want %#x, 1", r828d.addr7, r828d.vcoTarget, r828dAddr7)
	}
}

func TestOpenNoTuner(t *testing.T) {
	tr := newSmokeTransport("Generic", "Nothing")
	delete(tr.i2c, 0x3a) // no chip answers
	if _, err := Open(tr); err == nil {
		t.Error("Open: expected an error when no tuner responds")
	}
}
