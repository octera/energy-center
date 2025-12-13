package regulation

import (
	"fmt"

	"ocpp-server/internal/config"

	"github.com/sirupsen/logrus"
)

// RegulationType type de régulateur
type RegulationType string

const (
	PIDRegulation    RegulationType = "pid"
	SimpleRegulation RegulationType = "simple"
)

// CreateRegulator factory pour créer des régulateurs
func CreateRegulator(regulationType RegulationType, cfg *config.Config, logger *logrus.Logger) (RegulationService, error) {
	switch regulationType {
	case PIDRegulation:
		pidConfig := PIDConfig{
			Kp:               cfg.Charging.PIDKp,
			Ki:               cfg.Charging.PIDKi,
			Kd:               cfg.Charging.PIDKd,
			SmoothingFactor:  cfg.Charging.SmoothingFactor,
			MaxTimeGap:       60.0,  // 1 minute max entre mesures
			SurplusThreshold: 100.0, // 100W de surplus minimum
			ImportThreshold:  50.0,  // 50W d'import maximum
		}
		return NewPIDRegulator(pidConfig, logger), nil

	case SimpleRegulation:
		simpleConfig := SimpleConfig{
			SurplusThreshold: 200.0, // 200W de surplus pour démarrer
			HysteresisMargin: 100.0, // 100W d'hystérésis
		}
		return NewSimpleRegulator(simpleConfig, logger), nil

	default:
		return nil, fmt.Errorf("unknown regulation type: %s", regulationType)
	}
}
