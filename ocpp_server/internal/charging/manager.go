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
	// Créer le régulateur PID par défaut
	regulator, err := regulation.CreateRegulator(regulation.PIDRegulation, cfg, logger)
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

	// Préparer les données d'entrée pour le régulateur
	input := regulation.RegulationInput{
		GridPower:     gridPower,
		IsOffPeak:     isOffPeak,
		MaxCurrent:    m.config.Charging.MaxTotalCurrent,
		MaxHousePower: m.config.Charging.MaxHousePower,
		TargetPower:   m.config.Charging.GridTargetPower,
		Timestamp:     gridTimestamp,
	}

	// Calculer le courant cible via le régulateur
	output := m.regulator.Calculate(input)

	m.logger.Debugf("Regulation: %s - Target: %.1fA, Reason: %s",
		m.regulator.GetName(), output.TargetCurrent, output.Reason)

	// Distribuer le courant aux stations
	connectedStations := m.getConnectedStations()
	if len(connectedStations) == 0 {
		m.logger.Debug("No connected stations")
		return
	}

	if output.TargetCurrent <= 0 {
		m.logger.Debug("No available current from regulator, stopping all charging")
		m.stopAllCharging()
		return
	}

	m.distributeCurrentByPriority(connectedStations, output.TargetCurrent)
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
