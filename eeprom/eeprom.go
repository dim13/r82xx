// Package eeprom implements the RTL2832U EEPROM access and layout
// encoding (RFC §5.5, §5.6): an I2C device at 7-bit address 0x50,
// 256 bytes, byte-by-byte, no burst reads.
package eeprom

import (
	"errors"
	"time"

	"github.com/dim13/r82xx/i2c"
	"github.com/dim13/r82xx/wire"
)

const (
	addr7 = 0x50

	writeDelay = 5 * time.Millisecond
)

// ErrTruncated means the manufacturer/product/serial strings don't fit
// the layout's 69-byte window.
var ErrTruncated = errors.New("eeprom: string descriptors exceed 69-byte window")

// Read reads n bytes starting at offset off.
func Read(t wire.Transport, off uint8, n int) ([]byte, error) {
	buf := make([]byte, n)
	for i := range buf {
		b, err := i2c.Read(t, addr7, off+uint8(i), 1)
		if err != nil {
			return nil, err
		}
		buf[i] = b[0]
	}
	return buf, nil
}

// Write writes data starting at offset off, skipping a byte whose
// value already matches to avoid unnecessary EEPROM wear.
func Write(t wire.Transport, off uint8, data []byte) error {
	for i, want := range data {
		addr := off + uint8(i)
		cur, err := i2c.Read(t, addr7, addr, 1)
		if err != nil {
			return err
		}
		if cur[0] == want {
			continue
		}
		if err := i2c.Write(t, addr7, addr, []byte{want}); err != nil {
			return err
		}
		time.Sleep(writeDelay)
	}
	return nil
}

// Layout is the fixed 79-byte header of the 256-byte EEPROM content
// (RFC §5.6); Manufacturer/Product feed board identification (§4, §8).
type Layout struct {
	VendorID     uint16
	ProductID    uint16
	SerialEnable bool // 0xA5 = enabled, 0x00 = disabled
	RemoteWakeup bool
	IREndpoint   bool
	Manufacturer string
	Product      string
	Serial       string
}

const (
	magic0, magic1 = 0x28, 0x32
	cfgBase        = 0x14 // base value of the config-flags byte, remaining bits fixed
)

// DecodeLayout parses the first 79 bytes of an EEPROM dump.
func DecodeLayout(b []byte) (Layout, error) {
	if len(b) < 79 || b[0] != magic0 || b[1] != magic1 {
		return Layout{}, errors.New("eeprom: bad header magic")
	}
	l := Layout{
		VendorID:     uint16(b[2]) | uint16(b[3])<<8,
		ProductID:    uint16(b[4]) | uint16(b[5])<<8,
		SerialEnable: b[6] == 0xa5,
		RemoteWakeup: b[7]&0x01 != 0,
		IREndpoint:   b[7]&0x02 != 0,
	}
	strs, err := decodeUSBStrings(b[9:78])
	if err != nil {
		return Layout{}, err
	}
	if len(strs) > 0 {
		l.Manufacturer = strs[0]
	}
	if len(strs) > 1 {
		l.Product = strs[1]
	}
	if len(strs) > 2 {
		l.Serial = strs[2]
	}
	return l, nil
}

// EncodeLayout renders l as the first 79 bytes of an EEPROM dump.
func EncodeLayout(l Layout) ([]byte, error) {
	strs, err := encodeUSBStrings(l.Manufacturer, l.Product, l.Serial)
	if err != nil {
		return nil, err
	}
	if len(strs) > 69 {
		return nil, ErrTruncated
	}
	b := make([]byte, 79)
	b[0], b[1] = magic0, magic1
	b[2], b[3] = byte(l.VendorID), byte(l.VendorID>>8)
	b[4], b[5] = byte(l.ProductID), byte(l.ProductID>>8)
	if l.SerialEnable {
		b[6] = 0xa5
	}
	cfg := byte(cfgBase)
	if l.RemoteWakeup {
		cfg |= 0x01
	}
	if l.IREndpoint {
		cfg |= 0x02
	}
	b[7] = cfg
	b[8] = 0x02 // fixed; purpose unconfirmed (RFC §15 item 2)
	copy(b[9:78], strs)
	b[78] = 0x00 // IR-config-block length, always "no IR config"
	return b, nil
}

// Only ASCII is supported: the high byte of every UTF-16 code unit is
// assumed zero.
func decodeUSBStrings(b []byte) ([]string, error) {
	var out []string
	for len(b) > 0 {
		n := int(b[0])
		if n == 0 {
			break
		}
		if n > len(b) || n < 2 {
			return nil, errors.New("eeprom: truncated string descriptor")
		}
		chars := (n - 2) / 2
		s := make([]byte, chars)
		for i := range s {
			s[i] = b[2+2*i]
		}
		out = append(out, string(s))
		b = b[n:]
	}
	return out, nil
}

func encodeUSBStrings(strs ...string) ([]byte, error) {
	var out []byte
	for _, s := range strs {
		n := 2 + 2*len(s)
		if n > 255 {
			return nil, ErrTruncated
		}
		d := make([]byte, n)
		d[0], d[1] = byte(n), 0x03
		for i := 0; i < len(s); i++ {
			d[2+2*i] = s[i]
		}
		out = append(out, d...)
	}
	return out, nil
}
