package regulation

import (
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// DeltaPIDConfig configuration du régulateur PID avec approche delta
type DeltaPIDConfig struct {
	Kp               float64 // Gain proportionnel
	Ki               float64 // Gain intégral
	Kd               float64 // Gain dérivé
	SmoothingFactor  float64 // Facteur de lissage de la puissance
	MaxTimeGap       float64 // Gap max entre mesures avant reset (secondes)
	SurplusThreshold float64 // Seuil de surplus pour autoriser la charge (W)
	ImportThreshold  float64 // Seuil d'import pour réduire la charge (W)
	MaxDeltaPerStep  float64 // Delta maximum par étape (A)
}

// DeltaRegulator implémentation PID avec calcul de delta au lieu de valeur absolue
type DeltaRegulator struct {
	config DeltaPIDConfig
	logger *logrus.Logger
	mutex  sync.RWMutex

	// État interne du PID
	previousError float64
	integralError float64
	smoothedPower float64
	lastUpdate    time.Time
	resetCount    int64
}

func NewDeltaRegulator(config DeltaPIDConfig, logger *logrus.Logger) *DeltaRegulator {
	return &DeltaRegulator{
		config:     config,
		logger:     logger,
		lastUpdate: time.Now(),
	}
}

func (d *DeltaRegulator) GetName() string {
	return "Delta PID Regulator"
}

func (d *DeltaRegulator) Calculate(input RegulationInput) RegulationOutput {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	// Mode HC : charge maximale sous contraintes
	if input.IsOffPeak {
		return d.calculateOffPeak(input)
	}

	// Mode HP : régulation PID avec delta
	return d.calculateOnPeakDelta(input)
}

func (d *DeltaRegulator) calculateOffPeak(input RegulationInput) RegulationOutput {
	// Mode HC : viser la charge maximale autorisée
	availablePower := input.MaxHousePower
	targetCurrent := availablePower / 230.0

	if targetCurrent > input.MaxCurrent {
		targetCurrent = input.MaxCurrent
	}

	// Delta = différence entre cible et actuel
	deltaCurrent := targetCurrent - input.CurrentCharging

	// Limiter le delta pour éviter des sauts brutaux
	if deltaCurrent > d.config.MaxDeltaPerStep {
		deltaCurrent = d.config.MaxDeltaPerStep
	}
	if deltaCurrent < -d.config.MaxDeltaPerStep {
		deltaCurrent = -d.config.MaxDeltaPerStep
	}

	return RegulationOutput{
		DeltaCurrent:  deltaCurrent,
		TargetCurrent: targetCurrent, // Pour compatibilité
		ShouldCharge:  targetCurrent > 6.0,
		Reason:        "Off-peak mode - adjusting to maximum charging",
		DebugInfo: map[string]interface{}{
			"available_power":  availablePower,
			"target_current":   targetCurrent,
			"current_charging": input.CurrentCharging,
			"delta":            deltaCurrent,
			"mode":             "HC",
		},
	}
}

func (d *DeltaRegulator) calculateOnPeakDelta(input RegulationInput) RegulationOutput {
	// Mise à jour du lissage
	d.updateSmoothedPower(input.GridPower, input.Timestamp)

	// Calcul de l'erreur PID - maintenant basé sur la puissance réelle
	// Puissance actuellement chargée
	chargingPower := input.CurrentCharging * 230.0

	// Erreur = puissance excédentaire (négative = surplus, positive = import)
	error := d.smoothedPower + chargingPower - input.TargetPower

	// Calcul du delta temps
	dt := input.Timestamp.Sub(d.lastUpdate).Seconds()

	// Reset si gap trop important
	if dt > d.config.MaxTimeGap {
		d.logger.Warnf("Delta PID: Large time gap (%.1fs), resetting controller", dt)
		d.reset()
		dt = 1.0
	}

	if dt <= 0 {
		dt = 1.0
	}

	// Calcul du delta PID
	deltaCurrent := d.calculatePIDDelta(error, dt)

	// Application des limites de sécurité
	deltaCurrent = d.applySafetyLimits(deltaCurrent, error, input)

	d.lastUpdate = input.Timestamp

	// Détermination de l'autorisation de charge
	shouldCharge := input.CurrentCharging > 0 || (error < -d.config.SurplusThreshold)

	// Création du résultat
	result := RegulationOutput{
		DeltaCurrent:  deltaCurrent,
		TargetCurrent: input.CurrentCharging + deltaCurrent, // Pour compatibilité
		ShouldCharge:  shouldCharge,
		DebugInfo: map[string]interface{}{
			"grid_power":       input.GridPower,
			"smoothed_power":   d.smoothedPower,
			"charging_power":   chargingPower,
			"current_charging": input.CurrentCharging,
			"error":            error,
			"delta_current":    deltaCurrent,
			"dt":               dt,
			"previous_error":   d.previousError,
			"integral_error":   d.integralError,
			"mode":             "HP",
		},
	}

	// Raison basée sur l'erreur
	if error > d.config.ImportThreshold {
		result.Reason = "Grid import detected - reducing charge"
	} else if error < -d.config.SurplusThreshold {
		result.Reason = "Surplus solar detected - increasing charge"
	} else if math.Abs(error) < 50 {
		result.Reason = "Near equilibrium - maintaining"
	} else if error > 0 {
		result.Reason = "Small import - slight reduction"
	} else {
		result.Reason = "Small surplus - slight increase"
	}

	d.logger.Debugf("Delta PID: Power=%.1fW, ChargingPower=%.1fW, Error=%.1fW, Delta=%.2fA, dt=%.1fs",
		d.smoothedPower, chargingPower, error, deltaCurrent, dt)

	return result
}

func (d *DeltaRegulator) updateSmoothedPower(currentPower float64, timestamp time.Time) {
	dt := timestamp.Sub(d.lastUpdate).Seconds()

	// Premier appel : initialisation directe
	if d.smoothedPower == 0 && dt < 1.0 {
		d.smoothedPower = currentPower
		return
	}

	if dt > 0 {
		alpha := 1.0 - math.Exp(-dt/d.config.SmoothingFactor)
		d.smoothedPower = alpha*currentPower + (1-alpha)*d.smoothedPower
	} else {
		d.smoothedPower = currentPower
	}
}

func (d *DeltaRegulator) calculatePIDDelta(error, dt float64) float64 {
	// Terme intégral
	d.integralError += error * dt

	// Terme dérivé
	derivative := (error - d.previousError) / dt

	// Calcul PID - directement en delta de courant
	deltaCurrent := d.config.Kp*error/230.0 + d.config.Ki*d.integralError/230.0 + d.config.Kd*derivative/230.0

	d.previousError = error

	return deltaCurrent
}

func (d *DeltaRegulator) applySafetyLimits(deltaCurrent, error float64, input RegulationInput) float64 {
	// Limitation du delta maximum par étape
	if deltaCurrent > d.config.MaxDeltaPerStep {
		deltaCurrent = d.config.MaxDeltaPerStep
	}
	if deltaCurrent < -d.config.MaxDeltaPerStep {
		deltaCurrent = -d.config.MaxDeltaPerStep
	}

	// Vérification que le résultat final ne dépasse pas les limites
	newCurrent := input.CurrentCharging + deltaCurrent
	if newCurrent < 0 {
		deltaCurrent = -input.CurrentCharging
		d.integralError = 0 // Anti-windup
	}
	if newCurrent > input.MaxCurrent {
		deltaCurrent = input.MaxCurrent - input.CurrentCharging
		d.integralError = 0 // Anti-windup
	}

	// Sécurité import : réduction agressive si import important
	if error > d.config.ImportThreshold && input.CurrentCharging > 0 {
		aggressiveReduction := math.Min(error/500.0, input.CurrentCharging*0.5)
		if deltaCurrent > -aggressiveReduction {
			deltaCurrent = -aggressiveReduction
		}
		d.integralError = 0
		d.logger.Debugf("Delta PID: Import detected (%.0fW), aggressive reduction %.1fA", error, aggressiveReduction)
	}

	return deltaCurrent
}

func (d *DeltaRegulator) Reset() {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.reset()
}

func (d *DeltaRegulator) reset() {
	d.previousError = 0
	d.integralError = 0
	d.resetCount++
	d.logger.Infof("Delta PID controller reset (count: %d)", d.resetCount)
}

func (d *DeltaRegulator) GetStatus() map[string]interface{} {
	d.mutex.RLock()
	defer d.mutex.RUnlock()

	return map[string]interface{}{
		"name":           d.GetName(),
		"config":         d.config,
		"previous_error": d.previousError,
		"integral_error": d.integralError,
		"smoothed_power": d.smoothedPower,
		"last_update":    d.lastUpdate,
		"reset_count":    d.resetCount,
	}
}
