package home_assistant

import "encoding/json"

type Unit int64

const (
	W Unit = iota
	kW
	V
	Wh
	kWh
	A
	VA
	Hz
)

func (s Unit) String() string {
	switch s {
	case W:
		return "W"
	case kW:
		return "Kw"
	case V:
		return "V"
	case Wh:
		return "Wh"
	case kWh:
		return "kWh"
	case A:
		return "A"
	case VA:
		return "VA"
	case Hz:
		return "Hz"
	}
	return "unknown"
}

func (s Unit) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}
