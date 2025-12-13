package main

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"ocpp-server/internal/charging"
	"ocpp-server/internal/config"
	"ocpp-server/internal/mqtt"
	"ocpp-server/internal/ocpp"

	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	logger.Infof("Starting OCPP server with config: %+v", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ocppServer := ocpp.NewServer(cfg, logger)

	chargingManager := charging.NewManager(cfg, logger)
	chargingManager.SetStations(ocppServer.GetStations())

	mqttClient, err := mqtt.NewClient(cfg, logger)
	if err != nil {
		logger.Fatalf("Failed to create MQTT client: %v", err)
	}

	chargingManager.SetGridData(mqttClient.GetGridData())
	chargingManager.SetHPHCState(mqttClient.GetHPHCState())

	ocppServer.SetCurrentLimitUpdateCallback(func(stationID string, limit float64) {
		logger.Infof("OCPP: Updated current limit for %s to %.1fA", stationID, limit)
	})

	chargingManager.SetCurrentLimitUpdateCallback(func(stationID string, limit float64) {
		err := ocppServer.UpdateCurrentLimit(stationID, limit)
		if err != nil {
			logger.Errorf("Failed to update OCPP current limit: %v", err)
		}
	})

	mqttClient.SetCallbacks(
		func(power float64) {
			logger.Debugf("MQTT: Grid power updated to %.2fW", power)
		},
		func(isOffPeak bool) {
			state := "HP"
			if isOffPeak {
				state = "HC"
			}
			logger.Infof("MQTT: HP/HC state updated to %s", state)
		},
	)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ocppServer.Start(ctx); err != nil {
			logger.Errorf("OCPP server error: %v", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		chargingManager.Start(ctx)
	}()

	if err := mqttClient.Connect(); err != nil {
		logger.Fatalf("Failed to connect to MQTT: %v", err)
	}
	defer mqttClient.Disconnect()

	logger.Info("All services started successfully")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		logger.Info("Received shutdown signal")
	case <-ctx.Done():
		logger.Info("Context cancelled")
	}

	logger.Info("Shutting down...")
	cancel()

	ocppServer.Stop()

	wg.Wait()
	logger.Info("Shutdown complete")
}
