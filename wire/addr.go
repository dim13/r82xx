package wire

// USB-core and system block register addresses.
const (
	USBSysCtl    uint16 = 0x2000 // USB block: system control
	USBEPACtl    uint16 = 0x2148 // USB block: bulk endpoint control
	USBEPAMaxPkt uint16 = 0x2158 // USB block: bulk endpoint max packet
	DemodCtl     uint16 = 0x3000 // system block: demodulator/ADC power
	DemodCtl1    uint16 = 0x300b // system block: demodulator control 1
)
