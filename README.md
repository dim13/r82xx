# r82xx

Go implementation of the RTL2832U + R820T/R828D USB SDR receiver, as
fitted to the RTL-SDR Blog V4.

## Scope

- RTL2832U demodulator: register access, baseband init, sample rate,
  IF tuning, bulk sample streaming.
- R820T/R828D tuner: PLL, RF band table, bandwidth, gain control.
- RTL-SDR Blog V4 board behavior: HF upconversion, notch filters,
  3-way RF input switching, fixed VGA gain.

Other tuner families (E4000, FC0012/FC0013, FC2580) are detected but
not driven — `Open` returns `tuner.ErrUnsupported` for them.

This package has no USB backend of its own. Callers supply one by
implementing `wire.Transport` (control transfers, bulk reads, USB
string descriptors) — wrap [`google/gousb`](https://github.com/google/gousb)
or any other libusb binding.

## Usage

```go
import (
    "github.com/dim13/r82xx"
    "github.com/dim13/r82xx/wire"
)

var t wire.Transport = myUSBTransport{} // your Transport implementation

dev, err := r82xx.Open(t)
if err != nil {
    log.Fatal(err)
}
defer dev.Close()

if _, err := dev.SetSampleRate(2_400_000); err != nil {
    log.Fatal(err)
}
if err := dev.SetFrequency(100_000_000); err != nil {
    log.Fatal(err)
}
if err := dev.SetGain(200); err != nil { // tenths of dB, see tuner.GainSteps
    log.Fatal(err)
}

buf := make([]byte, 262_144)
for {
    n, err := dev.Read(buf) // interleaved 8-bit I/Q pairs
    if err != nil {
        log.Fatal(err)
    }
    process(buf[:n])
}
```

`Device.ReadAsync` runs concurrent reads with a callback instead of a
blocking loop; see its doc comment for buffer-count/size defaults.
