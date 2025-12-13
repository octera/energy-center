package charging

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"ocpp-server/internal/config"
	"ocpp-server/internal/models"
	"ocpp-server/internal/regulation"

	"github.com/sirupsen/logrus"
)

type Manager struct {
	config *config.Config
	logger *logrus.Logger

	stations  map[string]*models.ChargingStation
	gridData  *models.GridData
	hphcState *models.HPHCState

	regulator regulation.RegulationService

	mutex sync.RWMutex

	onCurrentLimitUpdate func(stationID string, limit float64)
}

func NewManager(cfg *config.Config, logger *logrus.Logger) *Manager {
	// Créer le nouveau régulateur Delta PID par défaut
	regulator, err := regulation.CreateRegulator(regulation.DeltaPIDRegulation, cfg, logger)
	if err != nil {
		logger.Fatalf("Failed to create regulator: %v", err)
	}

	return &Manager{
		config:    cfg,
		logger:    logger,
		stations:  make(map[string]*models.ChargingStation),
		regulator: regulator,
	}
}

// SetRegulator permet de changer de régulateur
func (m *Manager) SetRegulator(regulator regulation.RegulationService) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.regulator = regulator
	m.logger.Infof("Switched to regulator: %s", regulator.GetName())
}

func (m *Manager) SetStations(stations map[string]*models.ChargingStation) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.stations = stations
}

func (m *Manager) SetGridData(gridData *models.GridData) {
	m.gridData = gridData
}

func (m *Manager) SetHPHCState(hphcState *models.HPHCState) {
	m.hphcState = hphcState
}

func (m *Manager) SetCurrentLimitUpdateCallback(callback func(string, float64)) {
	m.onCurrentLimitUpdate = callback
}

func (m *Manager) Start(ctx context.Context) {
	// Watchdog timer pour arrêter la charge si pas de message MQTT
	watchdogTicker := time.NewTicker(1 * time.Minute)
	defer watchdogTicker.Stop()

	m.logger.Info("Starting charging manager with MQTT-driven updates")

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Stopping charging manager")
			return
		case <-watchdogTicker.C:
			m.checkDataFreshness()
		}
	}
}

// Cette fonction sera appelée quand un message MQTT arrive
func (m *Manager) OnGridPowerUpdate() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.logger.Debug("Grid power updated via MQTT, triggering PID calculation")
	m.updateChargingLimitsInternal()
}

func (m *Manager) checkDataFreshness() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if m.gridData == nil || m.hphcState == nil {
		return
	}

	_, gridTimestamp := m.gridData.Get()
	_, hphcTimestamp := m.hphcState.Get()

	// Watchdog: Si pas de données depuis 5 minutes, arrêter la charge
	if time.Since(gridTimestamp) > 5*time.Minute {
		m.logger.Warn("No grid data received for 5 minutes, stopping charging for safety")
		m.stopAllCharging()
		m.regulator.Reset()
		return
	}

	if time.Since(hphcTimestamp) > 5*time.Minute {
		m.logger.Warn("No HP/HC data received for 5 minutes, stopping charging for safety")
		m.stopAllCharging()
		m.regulator.Reset()
		return
	}
}

// Version interne appelée avec le mutex déjà acquis
func (m *Manager) updateChargingLimitsInternal() {
	if m.gridData == nil || m.hphcState == nil {
		return
	}

	gridPower, gridTimestamp := m.gridData.Get()
	isOffPeak, hphcTimestamp := m.hphcState.Get()

	// Vérification rapide de fraîcheur (détaillée dans le watchdog)
	if time.Since(gridTimestamp) > 5*time.Minute || time.Since(hphcTimestamp) > 5*time.Minute {
		m.logger.Warn("Grid data or HP/HC data is too old, stopping charging")
		m.stopAllCharging()
		m.regulator.Reset()
		return
	}

	// Calculer le courant actuellement en charge
	currentCharging := m.getCurrentTotalCharging()

	// Préparer les données d'entrée pour le régulateur
	input := regulation.RegulationInput{
		GridPower:       gridPower,
		CurrentCharging: currentCharging,
		IsOffPeak:       isOffPeak,
		MaxCurrent:      m.config.Charging.MaxTotalCurrent,
		MaxHousePower:   m.config.Charging.MaxHousePower,
		TargetPower:     m.config.Charging.GridTargetPower,
		Timestamp:       gridTimestamp,
	}

	// Calculer le delta via le régulateur
	output := m.regulator.Calculate(input)

	m.logger.Debugf("Regulation: %s - Current: %.1fA, Delta: %+.2fA, Reason: %s",
		m.regulator.GetName(), currentCharging, output.DeltaCurrent, output.Reason)

	// Récupérer les stations connectées
	connectedStations := m.getConnectedStations()
	if len(connectedStations) == 0 {
		m.logger.Debug("No connected stations")
		return
	}

	// Gérer selon le type de régulateur
	if output.DeltaCurrent != 0 {
		// Nouveau régulateur Delta : appliquer le delta
		if !output.ShouldCharge && currentCharging > 0 {
			m.logger.Debug("Regulation indicates charging should stop")
			m.stopAllCharging()
			return
		}
		m.applyCurrentDelta(connectedStations, output.DeltaCurrent)
	} else {
		// Ancien régulateur : utiliser TargetCurrent (compatibilité)
		if output.TargetCurrent <= 0 {
			m.logger.Debug("No available current from regulator, stopping all charging")
			m.stopAllCharging()
			return
		}
		m.distributeCurrentByPriority(connectedStations, output.TargetCurrent)
	}
}

// getCurrentTotalCharging calcule le courant total actuellement en charge
func (m *Manager) getCurrentTotalCharging() float64 {
	total := 0.0
	for _, station := range m.stations {
		if station.IsConnected && station.IsCharging {
			total += station.GetCurrentLimit()
		}
	}
	return total
}

// applyCurrentDelta applique un delta de courant aux stations connectées
func (m *Manager) applyCurrentDelta(stations []*models.ChargingStation, deltaCurrent float64) {
	if math.Abs(deltaCurrent) < 0.1 {
		m.logger.Debug("Delta too small, no adjustment needed")
		return
	}

	m.logger.Debugf("Applying delta %.2fA to %d stations", deltaCurrent, len(stations))

	if deltaCurrent > 0 {
		// Augmentation: distribuer selon priorité et disponibilité
		m.distributePositiveDelta(stations, deltaCurrent)
	} else {
		// Réduction: réduire proportionnellement
		m.distributeNegativeDelta(stations, -deltaCurrent)
	}
}

// distributePositiveDelta distribue un surplus de courant
func (m *Manager) distributePositiveDelta(stations []*models.ChargingStation, deltaCurrent float64) {
	remaining := deltaCurrent

	for _, station := range stations {
		if remaining <= 0 {
			break
		}

		currentLimit := station.GetCurrentLimit()
		maxIncrease := station.MaxCurrent - currentLimit

		// Si station pas encore en charge, besoin d'au moins 6A
		if currentLimit == 0 {
			if remaining >= 6.0 && maxIncrease >= 6.0 {
				allocation := math.Min(remaining, maxIncrease)
				m.setStationCurrent(station.ID, allocation)
				remaining -= allocation
				m.logger.Infof("Started charging station %s with %.1fA", station.ID, allocation)
			}
		} else if maxIncrease > 0 {
			// Station déjà en charge, peut augmenter graduellement
			allocation := math.Min(remaining, maxIncrease)
			m.setStationCurrent(station.ID, currentLimit+allocation)
			remaining -= allocation
			m.logger.Infof("Increased station %s to %.1fA (+%.1fA)", station.ID, currentLimit+allocation, allocation)
		}
	}

	if remaining > 0 {
		m.logger.Debugf("Could not allocate %.1fA (stations at max)", remaining)
	}
}

// distributeNegativeDelta réduit le courant proportionnellement
func (m *Manager) distributeNegativeDelta(stations []*models.ChargingStation, reductionCurrent float64) {
	totalCharging := 0.0
	for _, station := range stations {
		if station.GetCurrentLimit() > 0 {
			totalCharging += station.GetCurrentLimit()
		}
	}

	if totalCharging == 0 {
		return
	}

	for _, station := range stations {
		currentLimit := station.GetCurrentLimit()
		if currentLimit > 0 {
			// Réduction proportionnelle
			reduction := reductionCurrent * (currentLimit / totalCharging)
			newLimit := math.Max(0, currentLimit-reduction)

			// Si tombe sous 6A, arrêter complètement
			if newLimit < 6.0 && newLimit > 0 {
				newLimit = 0
			}

			m.setStationCurrent(station.ID, newLimit)

			if newLimit == 0 {
				m.logger.Infof("Stopped charging station %s", station.ID)
			} else {
				m.logger.Infof("Reduced station %s to %.1fA (-%.1fA)", station.ID, newLimit, reduction)
			}
		}
	}
}

func (m *Manager) getConnectedStations() []*models.ChargingStation {
	var connected []*models.ChargingStation

	for _, station := range m.stations {
		if station.IsConnected {
			connected = append(connected, station)
		}
	}

	sort.Slice(connected, func(i, j int) bool {
		return connected[i].Priority < connected[j].Priority
	})

	return connected
}

func (m *Manager) distributeCurrentByPriority(stations []*models.ChargingStation, totalCurrent float64) {
	m.logger.Debugf("Distributing %.1fA among %d stations", totalCurrent, len(stations))

	remainingCurrent := totalCurrent

	for _, station := range stations {
		if remainingCurrent <= 0 {
			m.setStationCurrent(station.ID, 0)
			continue
		}

		minChargingCurrent := 6.0
		maxStationCurrent := station.MaxCurrent

		if remainingCurrent < minChargingCurrent {
			m.setStationCurrent(station.ID, 0)
			continue
		}

		allocatedCurrent := math.Min(remainingCurrent, maxStationCurrent)

		if allocatedCurrent >= minChargingCurrent {
			m.setStationCurrent(station.ID, allocatedCurrent)
			remainingCurrent -= allocatedCurrent
			m.logger.Infof("Allocated %.1fA to station %s (priority %d)", allocatedCurrent, station.ID, station.Priority)
		} else {
			m.setStationCurrent(station.ID, 0)
		}
	}

	if remainingCurrent > 0 {
		m.logger.Debugf("%.1fA remaining after distribution", remainingCurrent)
	}
}

func (m *Manager) setStationCurrent(stationID string, current float64) {
	station, exists := m.stations[stationID]
	if !exists {
		return
	}

	currentLimit := station.GetCurrentLimit()

	if math.Abs(current-currentLimit) < 0.5 {
		return
	}

	station.SetCurrentLimit(current)

	if m.onCurrentLimitUpdate != nil {
		m.onCurrentLimitUpdate(stationID, current)
	}
}

func (m *Manager) stopAllCharging() {
	for _, station := range m.stations {
		if station.GetCurrentLimit() > 0 {
			m.setStationCurrent(station.ID, 0)
		}
	}
}

func (m *Manager) GetStatus() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	status := make(map[string]interface{})

	if m.gridData != nil {
		gridPower, gridTimestamp := m.gridData.Get()
		status["grid_power"] = gridPower
		status["grid_timestamp"] = gridTimestamp
	}

	if m.hphcState != nil {
		isOffPeak, hphcTimestamp := m.hphcState.Get()
		status["is_off_peak"] = isOffPeak
		status["hphc_timestamp"] = hphcTimestamp
	}

	// Ajouter le statut du régulateur
	status["regulator"] = m.regulator.GetStatus()

	stations := make(map[string]interface{})
	totalCurrent := 0.0

	for id, station := range m.stations {
		stationStatus := map[string]interface{}{
			"connected":     station.IsConnected,
			"charging":      station.IsCharging,
			"current_limit": station.GetCurrentLimit(),
			"max_current":   station.MaxCurrent,
			"priority":      station.Priority,
		}

		if station.IsConnected {
			totalCurrent += station.GetCurrentLimit()
		}

		stations[id] = stationStatus
	}

	status["stations"] = stations
	status["total_current"] = totalCurrent
	status["max_total_current"] = m.config.Charging.MaxTotalCurrent

	return status
}
