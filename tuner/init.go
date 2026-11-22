package tuner

import "github.com/dim13/r82xx/demod"

// Standby / power-down sequence (RFC §10.9), triggered on device
// close only if the tuner had been initialized.
var standbyRegs = []struct {
	reg, val byte
}{
	{0x06, 0xb1},
	{0x05, 0xa0},
	{0x07, 0x3a},
	{0x08, 0x40},
	{0x09, 0xc0},
	{0x0a, 0x36},
	{0x0c, 0x35},
	{0x0f, 0x68},
	{0x11, 0x03},
	{0x17, 0xf4},
	{0x19, 0x0c},
}

// Standby runs the power-down register sequence and invalidates any
// cached operating-standard state, forcing a fresh filter
// self-calibration on the next power-up.
func (tu *Tuner) Standby() error {
	for _, r := range standbyRegs {
		if err := tu.writeReg(r.reg, r.val); err != nil {
			return err
		}
	}
	tu.lastInputBand = -1
	return nil
}

// powerUpDefaults is the fixed 27-byte power-on default block covering
// registers 0x05-0x1F (RFC Table 19: 0x0C = 0xE0 base | gain code 11).
// Registers 0x08/0x09 (image-rejection trim) are left at these fixed
// values permanently: no runtime image-rejection calibration is
// implemented, matching the reference behavior (RFC §10.10 step 5).
var powerUpDefaults = [27]byte{
	0x00, 0x12, 0x70, 0xc0, 0x40, 0xdb, 0x6b, 0xeb, // 0x05-0x0c
	0x53, 0x75, 0x68, 0x6c, 0xbb, 0x80, 0x31, 0x00, // 0x0d-0x14
	0x00, 0xc0, 0x30, 0x48, 0xec, 0x60, 0x00, 0x24, // 0x15-0x1c
	0xdd, 0x6e, 0x40, // 0x1d-0x1f
}

// PowerUp runs the tuner's power-up initialization sequence over the
// I2C repeater (RFC §10.10): writes the fixed default register block,
// runs filter self-calibration at the 56MHz reference / 3.570MHz
// IF-center scratch point, then sets the AGC clock. The defaults
// match the register state the reference driver leaves after its own
// power-up, as captured on the wire.
func (tu *Tuner) PowerUp() error {
	for i, v := range powerUpDefaults {
		if err := tu.writeReg(uint8(0x05+i), v); err != nil {
			return err
		}
	}
	tu.ifCenterHz = demod.DefaultIFHz
	tu.lastInputBand = -1

	if err := tu.LO(56_000_000); err != nil {
		return err
	}
	if _, err := tu.filterSelfCalibrate(); err != nil {
		return err
	}

	if err := tu.setField(0x1a, 0x30, 4, 0b10); err != nil { // 80ms AGC clock
		return err
	}
	if err := tu.setField(0x05, 0x60, 5, 0); err != nil { // clear RF-input-select bits
		return err
	}
	return tu.setField(0x06, 0x08, 3, 0)
}
