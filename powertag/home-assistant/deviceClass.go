package home_assistant

import "encoding/json"

type DeviceClass int64

const (
	ApparentPower DeviceClass = iota
	Current
	Energy
	EnergyStorage
	Frequency
	PowerFactor
	Power
	ReactivePower
	Voltage
)

func (s DeviceClass) String() string {
	switch s {
	case ApparentPower:
		return "apparent_power"
	case Current:
		return "current"
	case Energy:
		return "energy"
	case EnergyStorage:
		return "energy_storage"
	case Frequency:
		return "frequency"
	case PowerFactor:
		return "power_factor"
	case Power:
		return "power"
	case ReactivePower:
		return "reactive_power"
	case Voltage:
		return "voltage"
	}
	return "unknown"
}

func (s DeviceClass) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}
