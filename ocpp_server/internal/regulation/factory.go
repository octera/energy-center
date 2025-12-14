package regulation

import (
	"fmt"

	"ocpp-server/internal/config"

	"github.com/sirupsen/logrus"
)

// RegulationType type de régulateur
type RegulationType string

const (
	PIDRegulation      RegulationType = "pid"
	DeltaPIDRegulation RegulationType = "delta_pid"
	OpenEVSERegulation RegulationType = "openevse"
	SimpleRegulation   RegulationType = "simple"
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

	case DeltaPIDRegulation:
		deltaPIDConfig := DeltaPIDConfig{
			Kp:               cfg.Charging.PIDKp,
			Ki:               cfg.Charging.PIDKi,
			Kd:               cfg.Charging.PIDKd,
			SmoothingFactor:  cfg.Charging.SmoothingFactor,
			MaxTimeGap:       60.0,  // 1 minute max entre mesures
			SurplusThreshold: 200.0, // 200W de surplus minimum (plus stable)
			ImportThreshold:  100.0, // 100W d'import maximum (plus stable)
			MaxDeltaPerStep:  5.0,   // Max 5A de variation par étape
		}
		return NewDeltaRegulator(deltaPIDConfig, logger), nil

	case OpenEVSERegulation:
		openevseConfig := OpenEVSEConfig{
			ReservePowerW:    100.0,  // 100W de réserve pour éviter l'import
			HysteresisPowerW: 600.0,  // 600W d'hystérésis comme dans l'article
			MinChargeTimeS:   300.0,  // 5 minutes minimum de charge
			SmoothingAttackS: 30.0,   // 30s pour attaque (rapide)
			SmoothingDecayS:  120.0,  // 2min pour décroissance (lent)
			MinChargePowerW:  1400.0, // 1.4kW minimum pour démarrer (6A)
			PollIntervalS:    10.0,   // 10s comme OpenEVSE
			MaxDeltaPerStepA: 3.0,    // Max 3A de variation par étape
		}
		return NewOpenEVSERegulator(openevseConfig, logger), nil

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
