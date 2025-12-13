package models

import (
	"sync"
	"time"
)

type ChargingStation struct {
	ID            string
	IsConnected   bool
	IsCharging    bool
	CurrentLimit  float64
	MaxCurrent    float64
	Priority      int
	LastHeartbeat time.Time
	mutex         sync.RWMutex
}

func NewChargingStation(id string, priority int, maxCurrent float64) *ChargingStation {
	return &ChargingStation{
		ID:           id,
		IsConnected:  false,
		IsCharging:   false,
		CurrentLimit: 0,
		MaxCurrent:   maxCurrent,
		Priority:     priority,
	}
}

func (cs *ChargingStation) SetConnected(connected bool) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.IsConnected = connected
	if connected {
		cs.LastHeartbeat = time.Now()
	}
}

func (cs *ChargingStation) SetCharging(charging bool) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	cs.IsCharging = charging
}

func (cs *ChargingStation) SetCurrentLimit(limit float64) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()
	if limit > cs.MaxCurrent {
		limit = cs.MaxCurrent
	}
	cs.CurrentLimit = limit
}

func (cs *ChargingStation) GetCurrentLimit() float64 {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()
	return cs.CurrentLimit
}

func (cs *ChargingStation) IsActive() bool {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()
	return cs.IsConnected && cs.IsCharging
}

type GridData struct {
	Power     float64
	Timestamp time.Time
	mutex     sync.RWMutex
}

func NewGridData() *GridData {
	return &GridData{
		Power:     0,
		Timestamp: time.Now(),
	}
}

func (gd *GridData) Update(power float64) {
	gd.mutex.Lock()
	defer gd.mutex.Unlock()
	gd.Power = power
	gd.Timestamp = time.Now()
}

func (gd *GridData) Get() (float64, time.Time) {
	gd.mutex.RLock()
	defer gd.mutex.RUnlock()
	return gd.Power, gd.Timestamp
}

type HPHCState struct {
	IsOffPeak bool
	Timestamp time.Time
	mutex     sync.RWMutex
}

func NewHPHCState() *HPHCState {
	return &HPHCState{
		IsOffPeak: false,
		Timestamp: time.Now(),
	}
}

func (hc *HPHCState) Update(isOffPeak bool) {
	hc.mutex.Lock()
	defer hc.mutex.Unlock()
	hc.IsOffPeak = isOffPeak
	hc.Timestamp = time.Now()
}

func (hc *HPHCState) Get() (bool, time.Time) {
	hc.mutex.RLock()
	defer hc.mutex.RUnlock()
	return hc.IsOffPeak, hc.Timestamp
}
