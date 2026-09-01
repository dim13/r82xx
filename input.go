package r82xx

// applyPlainInputSwitch is the two-way RF input switch used on a
// non-Blog-V4 R828D board (RFC §10.7, "R828D-Specific RF Input
// Switching"), keyed by requested RF frequency (not LO frequency).
// Superseded by the Blog V4 three-way switch (§11.4) when board
// identification succeeds. A no-op for plain R820T: that chip has one
// RF input and no switch to program (register 0x05 bits 6:5 "always 0
// on plain R820T", RFC §10.2/§10.8).
func (tu *Tuner) applyPlainInputSwitch(requestedHz float64) error {
	if tu.chip != TunerR828D {
		return nil
	}
	insel := byte(0x00)
	if requestedHz > 345_000_000 {
		insel = 0x03 // register 0x05 bits 6:5 = 0x60 -> field value 0b11
	}
	return tu.setField(0x05, 0x60, 5, insel)
}
