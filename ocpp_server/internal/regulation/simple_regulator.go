package regulation

import (
	"sync"

	"github.com/sirupsen/logrus"
)

// SimpleConfig configuration du régulateur simple
type SimpleConfig struct {
	SurplusThreshold float64 // Seuil de surplus pour démarrer la charge (W)
	HysteresisMargin float64 // Marge d'hystérésis pour éviter les oscillations (W)
}

// SimpleRegulator régulateur simple sans PID (tout/rien avec hystérésis)
type SimpleRegulator struct {
	config     SimpleConfig
	logger     *logrus.Logger
	mutex      sync.RWMutex
	isCharging bool
}

func NewSimpleRegulator(config SimpleConfig, logger *logrus.Logger) *SimpleRegulator {
	return &SimpleRegulator{
		config: config,
		logger: logger,
	}
}

func (s *SimpleRegulator) GetName() string {
	return "Simple On/Off Regulator"
}

func (s *SimpleRegulator) Calculate(input RegulationInput) RegulationOutput {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Mode HC : charge maximale
	if input.IsOffPeak {
		availablePower := input.MaxHousePower
		availableCurrent := availablePower / 230.0

		if availableCurrent > input.MaxCurrent {
			availableCurrent = input.MaxCurrent
		}

		return RegulationOutput{
			DeltaCurrent:  0, // Mode HC gère directement le courant cible
			TargetCurrent: availableCurrent,
			ShouldCharge:  availableCurrent > 6.0,
			Reason:        "Off-peak mode - maximum charging",
			DebugInfo: map[string]interface{}{
				"mode":              "HC",
				"available_current": availableCurrent,
			},
		}
	}

	// Mode HP : logique simple avec hystérésis
	return s.calculateOnPeakSimple(input)
}

func (s *SimpleRegulator) calculateOnPeakSimple(input RegulationInput) RegulationOutput {
	var targetCurrent float64
	var reason string

	// Logique avec hystérésis
	if !s.isCharging {
		// Actuellement arrêté : démarrer si surplus suffisant
		if input.GridPower < -s.config.SurplusThreshold {
			surplusPower := -input.GridPower
			targetCurrent = surplusPower / 230.0
			if targetCurrent > input.MaxCurrent {
				targetCurrent = input.MaxCurrent
			}
			s.isCharging = true
			reason = "Starting charge - surplus detected"
		} else {
			targetCurrent = 0
			reason = "No surplus - staying stopped"
		}
	} else {
		// Actuellement en charge : arrêter si plus de surplus (avec hystérésis)
		stopThreshold := -(s.config.SurplusThreshold - s.config.HysteresisMargin)
		if input.GridPower > stopThreshold {
			targetCurrent = 0
			s.isCharging = false
			reason = "No more surplus - stopping charge"
		} else {
			// Continuer la charge
			surplusPower := -input.GridPower
			targetCurrent = surplusPower / 230.0
			if targetCurrent > input.MaxCurrent {
				targetCurrent = input.MaxCurrent
			}
			reason = "Continuing charge - surplus available"
		}
	}

	s.logger.Debugf("Simple: Power=%.1fW, Target=%.1fA, Charging=%v",
		input.GridPower, targetCurrent, s.isCharging)

	return RegulationOutput{
		DeltaCurrent:  0, // Simple régulateur calcule directement le courant cible
		TargetCurrent: targetCurrent,
		ShouldCharge:  targetCurrent > 6.0,
		Reason:        reason,
		DebugInfo: map[string]interface{}{
			"grid_power":        input.GridPower,
			"surplus_threshold": s.config.SurplusThreshold,
			"hysteresis_margin": s.config.HysteresisMargin,
			"is_charging":       s.isCharging,
			"mode":              "HP",
		},
	}
}

func (s *SimpleRegulator) Reset() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.isCharging = false
	s.logger.Info("Simple regulator reset")
}

func (s *SimpleRegulator) GetStatus() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return map[string]interface{}{
		"name":        s.GetName(),
		"config":      s.config,
		"is_charging": s.isCharging,
	}
}
