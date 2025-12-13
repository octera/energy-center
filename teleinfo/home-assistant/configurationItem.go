package home_assistant

type Device struct {
	Identifiers []string `json:"identifiers"`
	Name        string   `json:"name"`
}

type ConfigurationItem struct {
	DeviceClass       DeviceClass `json:"device_class,omitempty"`
	UnitOfMeasurement Unit        `json:"unit_of_measurement,omitempty"`
	Device            Device      `json:"device"`
	StateClass        string      `json:"state_class,omitempty"`
	UniqueId          string      `json:"unique_id"`
	Name              string      `json:"name"`
	StateTopic        string      `json:"state_topic"`
	ValueTemplate     string      `json:"value_template,omitempty"`
}
