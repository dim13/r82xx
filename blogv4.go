package r82xx

// RTL-SDR Blog V4 board-specific behavior (RFC §11), layered on top of
// plain R828D behavior and active only once board identification
// (§4/§8, manufacturer/product strings "RTLSDRBlog"/"Blog V4") has
// succeeded.

// blogV4CrystalHz overrides the R828D's usual 16.0MHz default (§11.1).
const blogV4CrystalHz = 28_800_000

// blogV4FixedVGACode: automatic IF/VGA AGC is never engaged on this
// board; VGA is pinned at manual code 8 (~16.3dB) regardless of
// whatever the host requests (§11.5) -- a board-validated design
// choice (spectral pumping / intermodulation with auto VGA), not a
// limitation to fix.
const blogV4FixedVGACode = 8

// UpconvertHF applies the HF upconversion of §11.2: a request below
// 28.8MHz is silently retuned by +28.8MHz. MUST run before IF
// placement, harmonic selection, and PLL programming.
func UpconvertHF(requestedHz float64) float64 {
	if requestedHz < blogV4CrystalHz {
		return requestedHz + blogV4CrystalHz
	}
	return requestedHz
}

// notchBands are the fixed-frequency protected bands where the Blog
// V4 front-end notch filter MUST be disengaged (§11.3), evaluated
// against the original, pre-upconversion requested frequency.
var notchBands = [][2]float64{
	{0, 2_200_000},
	{85_000_000, 112_000_000},
	{172_000_000, 242_000_000},
}

func notchEnabled(originalHz float64) bool {
	for _, b := range notchBands {
		if originalHz >= b[0] && originalHz <= b[1] {
			return false
		}
	}
	return true
}

// applyNotch overrides register 0x17 bit 3 with the Blog V4 notch
// decision, on top of whatever §10.4's band table already wrote there.
func (tu *Tuner) applyNotch(originalHz float64) error {
	return tu.setField(0x17, 0x08, 3, boolBit(notchEnabled(originalHz)))
}

// Three-way RF input band, keyed by original (pre-upconversion)
// requested frequency (RFC Table 22).
type inputBand int

const (
	inputHF inputBand = iota
	inputVHF
	inputUHF
)

func selectInputBand(originalHz float64) inputBand {
	switch {
	case originalHz <= 28_800_000:
		return inputHF
	case originalHz < 250_000_000:
		return inputVHF
	default:
		return inputUHF
	}
}

// applyInputSwitch writes the three-way RF input switch (§11.4) only
// when the band actually changed, to avoid unnecessary I2C traffic
// while sweeping frequency within a band.
func (tu *Tuner) applyInputSwitch(originalHz float64) error {
	band := selectInputBand(originalHz)
	if int(band) == tu.lastInputBand {
		return nil
	}
	var cable2, cable1, airDeselect byte
	switch band {
	case inputHF:
		cable2, cable1, airDeselect = 1, 0, 1
	case inputVHF:
		cable2, cable1, airDeselect = 0, 1, 1
	case inputUHF:
		cable2, cable1, airDeselect = 0, 0, 0
	}
	if err := tu.setField(0x06, 0x08, 3, cable2); err != nil {
		return err
	}
	insel := cable1<<1 | airDeselect
	if err := tu.setField(0x05, 0x60, 5, insel); err != nil {
		return err
	}
	tu.lastInputBand = int(band)
	return nil
}
