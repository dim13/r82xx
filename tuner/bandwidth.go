package tuner

// bwRow is one entry of the narrow (<=4.5MHz) bandwidth table (RFC
// Table 12, 18 rows).
type bwRow struct {
	nominalHz           float64
	deliveredIFHz       float64
	reg0a, reg0b, reg1e byte
}

var narrowBandwidthTable = []bwRow{
	{290_000, 1_925_000, 0x0f, 0xe7, 0x00},
	{375_000, 1_857_000, 0x0f, 0xe8, 0x00},
	{420_000, 2_121_000, 0x0f, 0xd7, 0x00},
	{470_000, 1_788_000, 0x0f, 0xe9, 0x00},
	{600_000, 1_706_000, 0x0f, 0xea, 0x00},
	{860_000, 1_558_000, 0x0f, 0xeb, 0x00},
	{950_000, 2_205_000, 0x0f, 0x88, 0x00},
	{1_100_000, 2_125_000, 0x0f, 0x89, 0x00},
	{1_200_000, 1_350_000, 0x0f, 0xee, 0x00},
	{1_300_000, 2_043_000, 0x0f, 0x8a, 0x00},
	{1_503_000, 1_276_000, 0x0f, 0xef, 0x60},
	{1_600_000, 1_900_000, 0x0f, 0x8b, 0x00},
	{1_753_000, 1_412_000, 0x0f, 0xcf, 0x60},
	{1_800_000, 1_400_000, 0x0f, 0xaf, 0x00},
	{1_953_000, 1_530_000, 0x0f, 0x8f, 0x60},
	{2_200_000, 1_600_000, 0x0f, 0x8f, 0x00},
	{3_000_000, 2_000_000, 0x04, 0x8f, 0x00},
	{5_000_000, 3_570_000, 0x0b, 0x6b, 0x00}, // default / init-time bandwidth
}

type bwPreset struct {
	minHz, maxHz  float64 // (minHz, maxHz], except the first row which is (7MHz, +inf)
	deliveredIFHz float64
	reg0a, reg0b  byte
}

// wideBandwidthPresets covers requested bandwidths above 4.5MHz (RFC
// Table 11).
var wideBandwidthPresets = []bwPreset{
	{7_000_000, 1 << 62, 4_570_000, 0x10, 0x0b},
	{6_000_000, 7_000_000, 4_570_000, 0x10, 0x2a},
	{4_500_000, 6_000_000, 3_570_000, 0x10, 0x6b},
}

// nearestNarrowRow picks the row whose nominal bandwidth is closest to
// hz, comparing against the midpoint between adjacent entries.
func nearestNarrowRow(hz float64) bwRow {
	rows := narrowBandwidthTable
	i := 0
	for i < len(rows)-1 {
		mid := (rows[i].nominalHz + rows[i+1].nominalHz) / 2
		if hz <= mid {
			break
		}
		i++
	}
	return rows[i]
}

// SetBandwidth applies the channel/IF filter bandwidth closest to hz
// (RFC §10.5) and returns the resulting delivered IF center, which
// callers MUST feed into the next LO retune (§10.3 step 3).
func (tu *Tuner) SetBandwidth(hz float64) (deliveredIFHz float64, err error) {
	if hz > 4_500_000 {
		for _, p := range wideBandwidthPresets {
			if hz > p.minHz && hz <= p.maxHz {
				if err := tu.writeReg(0x0a, p.reg0a); err != nil {
					return 0, err
				}
				if err := tu.writeReg(0x0b, p.reg0b); err != nil {
					return 0, err
				}
				tu.ifCenterHz = p.deliveredIFHz
				return p.deliveredIFHz, nil
			}
		}
	}
	row := nearestNarrowRow(hz)
	if err := tu.writeReg(0x0a, row.reg0a); err != nil {
		return 0, err
	}
	if err := tu.writeReg(0x0b, row.reg0b); err != nil {
		return 0, err
	}
	if err := tu.writeReg(0x1e, row.reg1e); err != nil {
		return 0, err
	}
	tu.ifCenterHz = row.deliveredIFHz
	return row.deliveredIFHz, nil
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
