package charging

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"

	"ocpp-server/internal/config"
	"ocpp-server/internal/models"

	"github.com/sirupsen/logrus"
)

type Manager struct {
	config *config.Config
	logger *logrus.Logger

	stations  map[string]*models.ChargingStation
	gridData  *models.GridData
	hphcState *models.HPHCState

	smoothedPower float64
	lastUpdate    time.Time

	// PID Controller variables for grid regulation
	targetGridPower float64
	previousError   float64
	integralError   float64
	currentTarget   float64

	mutex sync.RWMutex

	onCurrentLimitUpdate func(stationID string, limit float64)
}

func NewManager(cfg *config.Config, logger *logrus.Logger) *Manager {
	return &Manager{
		config:        cfg,
		logger:        logger,
		stations:      make(map[string]*models.ChargingStation),
		smoothedPower: 0,
		lastUpdate:    time.Now(),
	}
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
		m.resetPID()
		return
	}

	if time.Since(hphcTimestamp) > 5*time.Minute {
		m.logger.Warn("No HP/HC data received for 5 minutes, stopping charging for safety")
		m.stopAllCharging()
		m.resetPID()
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
		m.resetPID()
		return
	}

	m.updateSmoothedPower(gridPower)

	/*connectedStations := m.getConnectedStations()
	if len(connectedStations) == 0 {
		m.logger.Debug("No connected stations")
		return
	}*/

	availableCurrent := m.calculateAvailableCurrent(isOffPeak)

	if availableCurrent <= 0 {
		m.logger.Debug("No available current, stopping all charging")
		m.stopAllCharging()
		return
	}

	//m.distributeCurrentByPriority(connectedStations, availableCurrent)
}

func (m *Manager) updateSmoothedPower(currentPower float64) {
	now := time.Now()
	dt := now.Sub(m.lastUpdate).Seconds()

	if dt > 0 {
		alpha := 1.0 - math.Exp(-dt/m.config.Charging.SmoothingFactor)
		m.smoothedPower = alpha*currentPower + (1-alpha)*m.smoothedPower
	}

	m.lastUpdate = now

	m.logger.Debugf("Grid power: %.2fW, Smoothed: %.2fW", currentPower, m.smoothedPower)
}

func (m *Manager) calculateAvailableCurrent(isOffPeak bool) float64 {
	maxCurrent := m.config.Charging.MaxTotalCurrent

	if isOffPeak {
		availablePower := m.config.Charging.MaxHousePower
		availableCurrent := availablePower / 230.0

		if availableCurrent > maxCurrent {
			availableCurrent = maxCurrent
		}

		m.logger.Debugf("Off-peak mode: available current %.1fA (limited by house power)", availableCurrent)
		return availableCurrent
	}

	// On-peak mode: Use PID controller to regulate grid power to target (typically 0W)
	return m.calculatePIDCurrent(maxCurrent)
}

func (m *Manager) calculatePIDCurrent(maxCurrent float64) float64 {
	targetPower := m.config.Charging.GridTargetPower
	currentPower := m.smoothedPower

	// PID Error calculation
	error := targetPower - currentPower // Negative error = surplus (good), positive = import (bad)

	// Time delta depuis la dernière mesure MQTT (pas depuis la dernière boucle!)
	now := time.Now()
	dt := now.Sub(m.lastUpdate).Seconds()

	// Si trop de temps s'est écoulé, on reset le PID pour éviter la divergence
	if dt > 60 { // Plus d'1 minute sans mesure
		m.logger.Warn("Large time gap since last measurement, resetting PID")
		m.resetPID()
		dt = 1.0
	}

	if dt <= 0 {
		dt = 1.0
	}

	// Integral term (accumulated error) - seulement si pas trop de temps écoulé
	m.integralError += error * dt

	// Derivative term (rate of change of error)
	derivative := (error - m.previousError) / dt

	// PID output (desired power adjustment)
	kp := m.config.Charging.PIDKp
	ki := m.config.Charging.PIDKi
	kd := m.config.Charging.PIDKd

	powerAdjustment := kp*error + ki*m.integralError + kd*derivative

	// Convert power adjustment to current adjustment
	currentAdjustment := powerAdjustment / 230.0

	// Update target current
	m.currentTarget += currentAdjustment

	// Clamp to limits
	if m.currentTarget < 0 {
		m.currentTarget = 0
		m.integralError = 0 // Anti-windup
	}
	if m.currentTarget > maxCurrent {
		m.currentTarget = maxCurrent
		m.integralError = 0 // Anti-windup
	}

	// Safety: In HP mode, only charge if we detect surplus over time
	if error < -100 { // More than 100W surplus
		// We have surplus, charging is allowed
	} else if error > 50 { // Importing more than 50W
		// Importing from grid, reduce charging aggressively
		m.currentTarget = math.Max(0, m.currentTarget-1.0)
		m.integralError = 0
	}

	m.previousError = error

	m.logger.Debugf("PID: Power=%.1fW, Error=%.1fW, Target=%.1fA, Adjustment=%.2fA, dt=%.1fs",
		currentPower, error, m.currentTarget, currentAdjustment, dt)

	return m.currentTarget
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
		status["smoothed_power"] = m.smoothedPower
	}

	if m.hphcState != nil {
		isOffPeak, hphcTimestamp := m.hphcState.Get()
		status["is_off_peak"] = isOffPeak
		status["hphc_timestamp"] = hphcTimestamp
	}

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

func (m *Manager) resetPID() {
	m.previousError = 0
	m.integralError = 0
	m.currentTarget = 0
	m.logger.Info("PID controller reset")
}
