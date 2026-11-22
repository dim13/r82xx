package wire

// USB-core and system block register addresses, as used by the
// reference driver on the wire.
const (
	USBSysCtl    uint16 = 0x2000 // USB block: system control
	USBEPACtl    uint16 = 0x2148 // USB block: bulk endpoint control
	USBEPAMaxPkt uint16 = 0x2158 // USB block: bulk endpoint max packet
	DemodCtl     uint16 = 0x3000 // system block: demodulator/ADC power
	DemodCtl1    uint16 = 0x300b // system block: demodulator control 1
	GPO          uint16 = 0x3001 // system block: GPIO output
	GPI          uint16 = 0x3002 // system block: GPIO input
	GPOE         uint16 = 0x3003 // system block: GPIO output enable
	GPD          uint16 = 0x3004 // system block: GPIO pull/disable
)
