package tuner

// ifLowPassHz are the IF low-pass filter corners selectable in
// register 0x0B bits 3:0 (code 15-i), widest first.
var ifLowPassHz = [...]float64{
	1_700_000, 1_600_000, 1_550_000, 1_450_000, 1_200_000,
	900_000, 700_000, 550_000, 450_000, 350_000,
}

// IF high-pass filter steps (register 0x0B bits 6:5).
const (
	hpStep1Hz = 350_000
	hpStep2Hz = 380_000
)

// SetBandwidth programs the IF filter for a channel of hz (RFC §10.5)
// and returns the IF center the tuner now delivers, which the caller
// must also program into the demodulator. Follows the reference
// driver as observed on the wire: wide presets above ~2.43MHz,
// otherwise high-pass steps plus the nearest low-pass corner around a
// 2.3MHz base IF. Registers 0x0A/0x0B are written masked, so their
// other fields (filter current, calibration code) survive.
func (tu *Tuner) SetBandwidth(hz float64) (deliveredIFHz float64, err error) {
	var reg0a, reg0b byte
	switch {
	case hz > 7_000_000:
		reg0a, reg0b, deliveredIFHz = 0x10, 0x0b, 4_570_000
	case hz > 6_000_000:
		reg0a, reg0b, deliveredIFHz = 0x10, 0x2a, 4_570_000
	case hz > ifLowPassHz[0]+hpStep1Hz+hpStep2Hz:
		reg0a, reg0b, deliveredIFHz = 0x10, 0x6b, 3_570_000
	default:
		reg0b = 0xa0 // 0x20: high-pass step 2 off, always below the presets
		ifHz, realBW := 2_300_000.0, 0.0
		if hz > ifLowPassHz[0]+hpStep1Hz {
			hz -= hpStep1Hz
			ifHz += hpStep1Hz
			realBW = hpStep1Hz
		} else {
			reg0b |= 0x40 // high-pass step 1 off
		}
		// Narrowest corner still wider than hz; the reference indexes
		// one before the first narrower corner, clamped here at the
		// widest.
		i := 0
		for i < len(ifLowPassHz) && hz <= ifLowPassHz[i] {
			i++
		}
		i = max(i-1, 0)
		reg0b |= byte(15 - i)
		realBW += ifLowPassHz[i]
		deliveredIFHz = ifHz - realBW/2
	}
	if err := tu.setField(0x0a, 0x10, 0, reg0a); err != nil {
		return 0, err
	}
	if err := tu.setField(0x0b, 0xef, 0, reg0b); err != nil {
		return 0, err
	}
	tu.ifCenterHz = deliveredIFHz
	return deliveredIFHz, nil
}

// filterSelfCalibrate runs the calibration procedure of RFC §10.5
// (steps 1-7): a fixed 56MHz synthesizer stimulus and IF center are
// the caller's responsibility (via LO/SetBandwidth) around this call,
// matching Device.Open's use at Phase A.5 / §10.10 step 7.
func (tu *Tuner) filterSelfCalibrate() (code byte, err error) {
	if err := tu.setField(0x0f, 0x04, 2, 1); err != nil { // cal clock enable
		return 0, err
	}
	for range 2 {
		if err := tu.setField(0x0b, 0x10, 4, 1); err != nil { // trigger pulse
			return 0, err
		}
		if err := tu.setField(0x0b, 0x10, 4, 0); err != nil {
			return 0, err
		}
		v, err := tu.readStatus(0x04)
		if err != nil {
			return 0, err
		}
		code = v & 0x0f
		if code != 0 && code != 15 {
			break
		}
		code = 0 // fall back to narrowest if both attempts stay invalid
	}
	if err := tu.setField(0x0f, 0x04, 2, 0); err != nil { // cal clock disable
		return 0, err
	}
	return code, tu.setField(0x0a, 0x1f, 0, code|0x10) // low nibble + fixed low-Q bit(4)
}
