package wire

// Block selects the internal address space a generic register access
// targets (RFC Table 2). Only the blocks accessed from outside this
// package (Sys, USB, I2C) are exported constants.
type Block uint16

// Block values (RFC Table 2).
const (
	BlockDemod Block = 0 // demodulator, legacy flat access; superseded by demod's paged scheme
	BlockUSB   Block = 1 // USB core
	BlockSys   Block = 2 // system / GPIO
	BlockTuner Block = 3 // tuner; unused directly, I2C block used instead
	BlockROM   Block = 4 // EEPROM
	BlockIR    Block = 5 // infrared; aliased into the system block's address space
	BlockI2C   Block = 6 // I2C bus
)

// IR is not addressed directly: it's wired as a sub-page of the
// system block (RFC §5.2).
func (b Block) readIndex() uint16 {
	if b == BlockIR {
		return uint16(BlockSys)<<8 | 0x01
	}
	return uint16(b) << 8
}

func (b Block) writeIndex() uint16 {
	if b == BlockIR {
		return uint16(BlockSys)<<8 | 0x11
	}
	return uint16(b)<<8 | 0x10
}
