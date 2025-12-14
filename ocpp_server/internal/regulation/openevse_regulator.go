package regulation

import (
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// OpenEVSEConfig configuration du régulateur OpenEVSE
type OpenEVSEConfig struct {
	ReservePowerW    float64 // Puissance réservée pour éviter l'import (W)
	HysteresisPowerW float64 // Hystérésis pour éviter les oscillations (W)
	MinChargeTimeS   float64 // Temps minimum de charge une fois démarrée (secondes)
	SmoothingAttackS float64 // Constante de temps d'attaque pour lissage (secondes)
	SmoothingDecayS  float64 // Constante de temps de décroissance pour lissage (secondes)
	MinChargePowerW  float64 // Puissance minimum pour démarrer la charge (W)
	PollIntervalS    float64 // Intervalle de polling (secondes)
	MaxDeltaPerStepA float64 // Delta maximum par étape (A)
}

// OpenEVSERegulator implémentation du régulateur OpenEVSE avec approche temporelle
type OpenEVSERegulator struct {
	config OpenEVSEConfig
	logger *logrus.Logger
	mutex  sync.RWMutex

	// État interne temporel
	isCharging          bool
	chargingStartTime   time.Time
	lastUpdateTime      time.Time
	smoothedExcessPower float64
	lastTargetCurrent   float64

	// Statistiques
	activationCount   int64
	deactivationCount int64
}

func NewOpenEVSERegulator(config OpenEVSEConfig, logger *logrus.Logger) *OpenEVSERegulator {
	return &OpenEVSERegulator{
		config:         config,
		logger:         logger,
		lastUpdateTime: time.Now(),
	}
}

func (o *OpenEVSERegulator) GetName() string {
	return "OpenEVSE-style Regulator"
}

func (o *OpenEVSERegulator) Calculate(input RegulationInput) RegulationOutput {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	// Mode HC : charge maximale sous contraintes
	if input.IsOffPeak {
		return o.calculateOffPeak(input)
	}

	// Mode HP : régulation OpenEVSE
	return o.calculateOpenEVSELogic(input)
}

func (o *OpenEVSERegulator) calculateOffPeak(input RegulationInput) RegulationOutput {
	// Mode HC : simple, viser la charge maximale
	availablePower := input.MaxHousePower
	targetCurrent := availablePower / 230.0

	if targetCurrent > input.MaxCurrent {
		targetCurrent = input.MaxCurrent
	}

	// Delta progressif vers la cible
	deltaCurrent := targetCurrent - input.CurrentCharging
	if math.Abs(deltaCurrent) > o.config.MaxDeltaPerStepA {
		if deltaCurrent > 0 {
			deltaCurrent = o.config.MaxDeltaPerStepA
		} else {
			deltaCurrent = -o.config.MaxDeltaPerStepA
		}
	}

	return RegulationOutput{
		DeltaCurrent:  deltaCurrent,
		TargetCurrent: targetCurrent,
		ShouldCharge:  targetCurrent > 6.0,
		Reason:        "Off-peak mode - maximum charging",
		DebugInfo: map[string]interface{}{
			"mode":            "HC",
			"target_current":  targetCurrent,
			"delta":           deltaCurrent,
			"available_power": availablePower,
		},
	}
}

func (o *OpenEVSERegulator) calculateOpenEVSELogic(input RegulationInput) RegulationOutput {
	dt := input.Timestamp.Sub(o.lastUpdateTime).Seconds()
	if dt <= 0 {
		dt = o.config.PollIntervalS // Valeur par défaut
	}

	// Calcul de la puissance excédentaire (algorithme OpenEVSE)
	chargingPower := input.CurrentCharging * 230.0
	excessPower := -input.GridPower + chargingPower // Surplus grid + puissance déjà en charge

	// Lissage temporel de la puissance excédentaire (comme OpenEVSE)
	o.updateSmoothedExcess(excessPower, dt)

	// Logique d'hystérésis OpenEVSE
	var deltaCurrent float64
	var reason string
	var shouldCharge bool

	if !o.isCharging {
		// Pas encore en charge : vérifier conditions de démarrage
		startThreshold := o.config.MinChargePowerW + o.config.HysteresisPowerW
		if o.smoothedExcessPower > startThreshold {
			// Conditions réunies pour démarrer
			shouldCharge = true
			o.isCharging = true
			o.chargingStartTime = input.Timestamp
			o.activationCount++

			// Calculer le courant cible basé sur l'excédent
			targetCurrent := o.calculateTargetCurrent(o.smoothedExcessPower)
			deltaCurrent = targetCurrent - input.CurrentCharging
			reason = "Starting charge - sufficient solar excess"

			o.logger.Infof("OpenEVSE: Starting charge - excess: %.0fW, target: %.1fA",
				o.smoothedExcessPower, targetCurrent)
		} else {
			// Pas assez de surplus
			shouldCharge = false
			deltaCurrent = 0
			if o.smoothedExcessPower > 0 {
				reason = "Insufficient surplus for charging"
			} else {
				reason = "Grid import detected - no charging"
			}
		}
	} else {
		// Déjà en charge : vérifier conditions d'arrêt et ajustement
		timeSinceStart := input.Timestamp.Sub(o.chargingStartTime).Seconds()
		stopThreshold := o.config.ReservePowerW

		if o.smoothedExcessPower < stopThreshold && timeSinceStart > o.config.MinChargeTimeS {
			// Arrêter la charge (hystérésis + temps minimum écoulé)
			shouldCharge = false
			o.isCharging = false
			o.deactivationCount++
			deltaCurrent = -input.CurrentCharging // Arrêt complet
			reason = "Stopping charge - insufficient excess power"

			o.logger.Infof("OpenEVSE: Stopping charge after %.1fs - excess: %.0fW",
				timeSinceStart, o.smoothedExcessPower)
		} else {
			// Continuer la charge avec ajustement
			shouldCharge = true
			targetCurrent := o.calculateTargetCurrent(o.smoothedExcessPower)

			// Lissage temporel des changements (attaque/décroissance)
			rawDelta := targetCurrent - input.CurrentCharging
			smoothedDelta := o.applySmoothingConstraints(rawDelta, dt)
			deltaCurrent = smoothedDelta

			if timeSinceStart < o.config.MinChargeTimeS {
				reason = "Maintaining charge - within minimum time"
			} else {
				reason = "Adjusting charge rate - following solar production"
			}
		}
	}

	// Limiter le delta pour éviter des sauts brutaux
	if math.Abs(deltaCurrent) > o.config.MaxDeltaPerStepA {
		if deltaCurrent > 0 {
			deltaCurrent = o.config.MaxDeltaPerStepA
		} else {
			deltaCurrent = -o.config.MaxDeltaPerStepA
		}
	}

	o.lastUpdateTime = input.Timestamp
	o.lastTargetCurrent = input.CurrentCharging + deltaCurrent

	return RegulationOutput{
		DeltaCurrent:  deltaCurrent,
		TargetCurrent: o.lastTargetCurrent,
		ShouldCharge:  shouldCharge,
		Reason:        reason,
		DebugInfo: map[string]interface{}{
			"mode":               "HP_OpenEVSE",
			"excess_power":       excessPower,
			"smoothed_excess":    o.smoothedExcessPower,
			"is_charging":        o.isCharging,
			"time_since_start":   input.Timestamp.Sub(o.chargingStartTime).Seconds(),
			"activation_count":   o.activationCount,
			"deactivation_count": o.deactivationCount,
			"dt":                 dt,
			"delta":              deltaCurrent,
		},
	}
}

// updateSmoothedExcess applique le lissage temporel OpenEVSE
func (o *OpenEVSERegulator) updateSmoothedExcess(excessPower, dt float64) {
	// Premier appel : initialisation directe
	if o.lastUpdateTime.IsZero() {
		o.smoothedExcessPower = excessPower
		return
	}

	// Lissage exponentiel avec constantes de temps différentes
	var timeConstant float64
	if excessPower > o.smoothedExcessPower {
		// Attaque (augmentation) : plus rapide
		timeConstant = o.config.SmoothingAttackS
	} else {
		// Décroissance : plus lent pour stabilité
		timeConstant = o.config.SmoothingDecayS
	}

	// Filtre passe-bas exponentiel
	alpha := 1.0 - math.Exp(-dt/timeConstant)
	o.smoothedExcessPower = alpha*excessPower + (1-alpha)*o.smoothedExcessPower
}

// calculateTargetCurrent calcule le courant cible basé sur l'excédent disponible
func (o *OpenEVSERegulator) calculateTargetCurrent(excessPower float64) float64 {
	// Soustraire la puissance de réserve
	availablePower := excessPower - o.config.ReservePowerW

	if availablePower <= 0 {
		return 0
	}

	// Convertir en courant
	targetCurrent := availablePower / 230.0

	// Appliquer les limites
	if targetCurrent < 6.0 {
		return 0 // En dessous du minimum, pas de charge
	}

	// Limiter au maximum autorisé
	if targetCurrent > 40.0 {
		targetCurrent = 40.0
	}

	return targetCurrent
}

// applySmoothingConstraints applique les contraintes de lissage temporel
func (o *OpenEVSERegulator) applySmoothingConstraints(rawDelta, dt float64) float64 {
	// Limiter la vitesse de changement (A/s)
	maxRateAS := o.config.MaxDeltaPerStepA / o.config.PollIntervalS
	maxDeltaThisStep := maxRateAS * dt

	if rawDelta > maxDeltaThisStep {
		return maxDeltaThisStep
	} else if rawDelta < -maxDeltaThisStep {
		return -maxDeltaThisStep
	}

	return rawDelta
}

func (o *OpenEVSERegulator) Reset() {
	o.mutex.Lock()
	defer o.mutex.Unlock()

	o.isCharging = false
	o.chargingStartTime = time.Time{}
	o.smoothedExcessPower = 0
	o.lastTargetCurrent = 0

	o.logger.Info("OpenEVSE regulator reset")
}

func (o *OpenEVSERegulator) GetStatus() map[string]interface{} {
	o.mutex.RLock()
	defer o.mutex.RUnlock()

	return map[string]interface{}{
		"name":                  o.GetName(),
		"config":                o.config,
		"is_charging":           o.isCharging,
		"charging_start_time":   o.chargingStartTime,
		"smoothed_excess_power": o.smoothedExcessPower,
		"last_target_current":   o.lastTargetCurrent,
		"activation_count":      o.activationCount,
		"deactivation_count":    o.deactivationCount,
		"last_update_time":      o.lastUpdateTime,
	}
}
