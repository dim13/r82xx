package tuner

// Type identifies which tuner family answered during detection (RFC
// §8, Table 6).
type Type int

const (
	None Type = iota
	E4000
	FC0013
	R820T
	R828D
	FC2580
	FC0012
)
