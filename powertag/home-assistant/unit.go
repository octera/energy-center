package home_assistant

import "encoding/json"

type Unit int64

const (
	None Unit = iota
	W
	kW
	V
	Wh
	KWh
	A
	VA
	Hz
)

func (s Unit) String() string {
	switch s {
	case None:
		return "None"
	case W:
		return "W"
	case kW:
		return "Kw"
	case V:
		return "V"
	case Wh:
		return "Wh"
	case KWh:
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
