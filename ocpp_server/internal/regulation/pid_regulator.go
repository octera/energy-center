package regulation

import (
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// PIDConfig configuration du régulateur PID
type PIDConfig struct {
	Kp               float64 // Gain proportionnel
	Ki               float64 // Gain intégral
	Kd               float64 // Gain dérivé
	SmoothingFactor  float64 // Facteur de lissage de la puissance
	MaxTimeGap       float64 // Gap max entre mesures avant reset (secondes)
	SurplusThreshold float64 // Seuil de surplus pour autoriser la charge (W)
	ImportThreshold  float64 // Seuil d'import pour réduire la charge (W)
}

// PIDRegulator implémentation PID de l'asservissement
type PIDRegulator struct {
	config PIDConfig
	logger *logrus.Logger
	mutex  sync.RWMutex

	// État interne du PID
	previousError float64
	integralError float64
	currentTarget float64
	smoothedPower float64
	lastUpdate    time.Time
	resetCount    int64
}

func NewPIDRegulator(config PIDConfig, logger *logrus.Logger) *PIDRegulator {
	return &PIDRegulator{
		config:     config,
		logger:     logger,
		lastUpdate: time.Now(),
	}
}

func (p *PIDRegulator) GetName() string {
	return "PID Regulator"
}

func (p *PIDRegulator) Calculate(input RegulationInput) RegulationOutput {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Mode HC : charge maximale sous contraintes
	if input.IsOffPeak {
		return p.calculateOffPeak(input)
	}

	// Mode HP : régulation PID
	return p.calculateOnPeak(input)
}

func (p *PIDRegulator) calculateOffPeak(input RegulationInput) RegulationOutput {
	// Mode HC : charge jusqu'à la limite de la maison ou du courant max
	availablePower := input.MaxHousePower
	availableCurrent := availablePower / 230.0

	if availableCurrent > input.MaxCurrent {
		availableCurrent = input.MaxCurrent
	}

	return RegulationOutput{
		TargetCurrent: availableCurrent,
		IsCharging:    availableCurrent > 6.0, // Courant minimum
		Reason:        "Off-peak mode - maximum charging",
		DebugInfo: map[string]interface{}{
			"available_power":   availablePower,
			"available_current": availableCurrent,
			"mode":              "HC",
		},
	}
}

func (p *PIDRegulator) calculateOnPeak(input RegulationInput) RegulationOutput {
	// Mise à jour du lissage
	p.updateSmoothedPower(input.GridPower, input.Timestamp)

	// Calcul de l'erreur PID
	// error > 0 = import (mauvais), error < 0 = surplus (bon)
	error := p.smoothedPower - input.TargetPower

	// Calcul du delta temps
	dt := input.Timestamp.Sub(p.lastUpdate).Seconds()

	// Reset si gap trop important
	if dt > p.config.MaxTimeGap {
		p.logger.Warnf("PID: Large time gap (%.1fs), resetting controller", dt)
		p.reset()
		dt = 1.0
	}

	if dt <= 0 {
		dt = 1.0
	}

	// Calcul PID
	pidOutput := p.calculatePID(error, dt)

	// Sécurité : vérification surplus/import
	safeOutput := p.applySafetyChecks(pidOutput, error, input.MaxCurrent)

	p.lastUpdate = input.Timestamp

	// Création du résultat
	result := RegulationOutput{
		TargetCurrent: safeOutput,
		IsCharging:    safeOutput > 6.0,
		DebugInfo: map[string]interface{}{
			"grid_power":     input.GridPower,
			"smoothed_power": p.smoothedPower,
			"error":          error,
			"pid_raw":        pidOutput,
			"pid_safe":       safeOutput,
			"dt":             dt,
			"previous_error": p.previousError,
			"integral_error": p.integralError,
			"mode":           "HP",
		},
	}

	if error > p.config.ImportThreshold {
		result.Reason = "Grid import detected - reducing charge"
	} else if error < -p.config.SurplusThreshold {
		result.Reason = "Surplus solar detected - charging"
	} else if error > 0 {
		result.Reason = "Small import - maintaining charge"
	} else {
		result.Reason = "Near equilibrium - maintaining"
	}

	p.logger.Debugf("PID: Power=%.1fW, Error=%.1fW, Target=%.1fA, dt=%.1fs",
		p.smoothedPower, error, safeOutput, dt)

	return result
}

func (p *PIDRegulator) updateSmoothedPower(currentPower float64, timestamp time.Time) {
	dt := timestamp.Sub(p.lastUpdate).Seconds()

	// Premier appel : initialisation directe
	if p.smoothedPower == 0 && dt < 1.0 {
		p.smoothedPower = currentPower
		return
	}

	if dt > 0 {
		alpha := 1.0 - math.Exp(-dt/p.config.SmoothingFactor)
		p.smoothedPower = alpha*currentPower + (1-alpha)*p.smoothedPower
	} else {
		p.smoothedPower = currentPower
	}
}

func (p *PIDRegulator) calculatePID(error, dt float64) float64 {
	// Terme intégral
	p.integralError += error * dt

	// Terme dérivé
	derivative := (error - p.previousError) / dt

	// Calcul PID - directement en courant
	pidOutputCurrent := p.config.Kp*error/230.0 + p.config.Ki*p.integralError/230.0 + p.config.Kd*derivative/230.0

	// Pour un surplus important, permettre un démarrage direct
	if error < -p.config.SurplusThreshold && p.currentTarget == 0 {
		// Démarrage direct basé sur le surplus disponible
		startCurrent := math.Min((-error)/230.0, 10.0) // Max 10A au démarrage
		p.currentTarget = startCurrent
		p.logger.Debugf("PID: Bootstrap start with %.1fA due to surplus", startCurrent)
	} else {
		// Mise à jour incrémentale normale
		p.currentTarget += pidOutputCurrent
	}

	p.previousError = error

	return p.currentTarget
}

func (p *PIDRegulator) applySafetyChecks(pidOutput, error, maxCurrent float64) float64 {
	// Limitation des bornes
	if pidOutput < 0 {
		pidOutput = 0
		p.integralError = 0 // Anti-windup
	}
	if pidOutput > maxCurrent {
		pidOutput = maxCurrent
		p.integralError = 0 // Anti-windup
	}

	// Sécurité import : réduction agressive seulement si on importe vraiment
	// error > 0 = import (mauvais), error < 0 = surplus (bon)
	if error > p.config.ImportThreshold && p.currentTarget > 0 {
		// Réduction proportionnelle à l'import
		reduction := math.Min(error/500.0, pidOutput*0.8) // Réduction max 80% du courant
		pidOutput = math.Max(0, pidOutput-reduction)
		p.integralError = 0
		p.logger.Debugf("PID: Import detected (%.0fW), reducing charge by %.1fA", error, reduction)
	}

	p.currentTarget = pidOutput
	return pidOutput
}

func (p *PIDRegulator) Reset() {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.reset()
}

func (p *PIDRegulator) reset() {
	p.previousError = 0
	p.integralError = 0
	p.currentTarget = 0
	p.resetCount++
	p.logger.Infof("PID controller reset (count: %d)", p.resetCount)
}

func (p *PIDRegulator) GetStatus() map[string]interface{} {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	return map[string]interface{}{
		"name":           p.GetName(),
		"config":         p.config,
		"previous_error": p.previousError,
		"integral_error": p.integralError,
		"current_target": p.currentTarget,
		"smoothed_power": p.smoothedPower,
		"last_update":    p.lastUpdate,
		"reset_count":    p.resetCount,
	}
}
