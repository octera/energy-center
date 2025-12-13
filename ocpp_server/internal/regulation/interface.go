package regulation

import (
	"time"
)

// RegulationInput contient les données d'entrée pour l'algorithme
type RegulationInput struct {
	GridPower     float64   // Puissance réseau actuelle (W)
	IsOffPeak     bool      // Mode HP/HC
	MaxCurrent    float64   // Courant maximum autorisé (A)
	MaxHousePower float64   // Puissance max maison (W)
	TargetPower   float64   // Consigne de puissance (généralement 0W)
	Timestamp     time.Time // Timestamp de la mesure
}

// RegulationOutput contient le résultat de l'algorithme
type RegulationOutput struct {
	TargetCurrent float64                // Courant cible calculé (A)
	IsCharging    bool                   // Autorisation de charge
	Reason        string                 // Raison de la décision
	DebugInfo     map[string]interface{} // Infos de debug
}

// RegulationService interface pour les algorithmes d'asservissement
type RegulationService interface {
	// Calculate calcule le courant cible basé sur l'entrée
	Calculate(input RegulationInput) RegulationOutput

	// Reset remet à zéro l'état interne de l'algorithme
	Reset()

	// GetName retourne le nom de l'algorithme
	GetName() string

	// GetStatus retourne l'état interne pour monitoring
	GetStatus() map[string]interface{}
}
