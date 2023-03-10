package teleinfo

import "github.com/tarm/serial"

func OpenPort(serialDevice string, mode string) (*serial.Port, error) {
	cfg := &serial.Config{
		Name:     serialDevice,
		Baud:     1200,
		Size:     7,
		Parity:   serial.ParityEven,
		StopBits: serial.Stop1,
	}
	if mode == "standard" {
		cfg.Baud = 9600
	}
	return serial.OpenPort(cfg)
}
