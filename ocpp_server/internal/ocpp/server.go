package ocpp

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"ocpp-server/internal/config"
	"ocpp-server/internal/models"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

type Server struct {
	server   *http.Server
	upgrader websocket.Upgrader
	stations map[string]*models.ChargingStation
	config   *config.Config
	logger   *logrus.Logger
	mutex    sync.RWMutex

	onCurrentLimitUpdate func(stationID string, limit float64)
}

func NewServer(cfg *config.Config, logger *logrus.Logger) *Server {
	s := &Server{
		stations: make(map[string]*models.ChargingStation),
		config:   cfg,
		logger:   logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	s.initializeStations()

	return s
}

func (s *Server) initializeStations() {
	station1 := models.NewChargingStation("station1", s.config.Charging.Station1Priority, 32.0)
	station2 := models.NewChargingStation("station2", s.config.Charging.Station2Priority, 32.0)

	s.stations[station1.ID] = station1
	s.stations[station2.ID] = station2

	s.logger.Info("Initialized charging stations")
}

func (s *Server) SetCurrentLimitUpdateCallback(callback func(string, float64)) {
	s.onCurrentLimitUpdate = callback
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/", s.handleWebSocket)

	addr := fmt.Sprintf("%s:%d", s.config.Server.Host, s.config.Server.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.logger.Infof("Starting OCPP WebSocket server on %s", addr)

	go func() {
		<-ctx.Done()
		s.logger.Info("Shutting down server...")
		s.server.Close()
	}()

	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server failed: %w", err)
	}

	return nil
}

func (s *Server) Stop() {
	if s.server != nil {
		s.logger.Info("Stopping OCPP server")
		s.server.Close()
	}
}

func (s *Server) GetStations() map[string]*models.ChargingStation {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[string]*models.ChargingStation)
	for k, v := range s.stations {
		result[k] = v
	}
	return result
}

func (s *Server) UpdateCurrentLimit(stationID string, limit float64) error {
	s.mutex.RLock()
	station, exists := s.stations[stationID]
	s.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("station %s not found", stationID)
	}

	station.SetCurrentLimit(limit)
	s.logger.Infof("Updated current limit for %s to %.1fA", stationID, limit)

	if s.onCurrentLimitUpdate != nil {
		s.onCurrentLimitUpdate(stationID, limit)
	}

	return nil
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	stationID := r.URL.Path[len("/ws/"):]
	if stationID == "" {
		http.Error(w, "Station ID required in path", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Errorf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	s.mutex.RLock()
	station, exists := s.stations[stationID]
	s.mutex.RUnlock()

	if !exists {
		s.logger.Warnf("Unknown station connected: %s", stationID)
	} else {
		station.SetConnected(true)
		s.logger.Infof("Station %s connected", stationID)
		defer func() {
			station.SetConnected(false)
			station.SetCharging(false)
			s.logger.Infof("Station %s disconnected", stationID)
		}()
	}

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			s.logger.Errorf("Read message error for %s: %v", stationID, err)
			break
		}

		if messageType == websocket.TextMessage {
			s.logger.Debugf("Received from %s: %s", stationID, string(message))

			response := s.handleOCPPMessage(stationID, message)
			if response != nil {
				err = conn.WriteMessage(websocket.TextMessage, response)
				if err != nil {
					s.logger.Errorf("Write message error for %s: %v", stationID, err)
					break
				}
			}
		}
	}
}

func (s *Server) handleOCPPMessage(stationID string, message []byte) []byte {
	s.logger.Debugf("Processing OCPP message from %s: %s", stationID, string(message))

	s.mutex.RLock()
	station, exists := s.stations[stationID]
	s.mutex.RUnlock()

	if exists {
		station.SetConnected(true)
	}

	response := `[3,"` + fmt.Sprintf("%d", time.Now().UnixNano()) + `",{}]`
	return []byte(response)
}
