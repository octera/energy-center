package regulation

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestPIDRegulator_OffPeakMode(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel) // Disable logs for tests

	config := PIDConfig{
		Kp:               0.001,
		Ki:               0.0001,
		Kd:               0.00001,
		SmoothingFactor:  0.1,
		MaxTimeGap:       60.0,
		SurplusThreshold: 100.0,
		ImportThreshold:  50.0,
	}

	regulator := NewPIDRegulator(config, logger)

	input := RegulationInput{
		GridPower:     1000, // Import
		IsOffPeak:     true, // HC mode
		MaxCurrent:    40.0,
		MaxHousePower: 12000.0,
		TargetPower:   0.0,
		Timestamp:     time.Now(),
	}

	output := regulator.Calculate(input)

	assert.True(t, output.IsCharging)
	assert.Equal(t, 40.0, output.TargetCurrent) // Limited by max current
	assert.Contains(t, output.Reason, "Off-peak")
	assert.Equal(t, "HC", output.DebugInfo["mode"])
}

func TestPIDRegulator_OnPeakNoSurplus(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)

	config := PIDConfig{
		Kp:               0.001,
		Ki:               0.0001,
		Kd:               0.00001,
		SmoothingFactor:  0.1,
		MaxTimeGap:       60.0,
		SurplusThreshold: 100.0,
		ImportThreshold:  50.0,
	}

	regulator := NewPIDRegulator(config, logger)

	input := RegulationInput{
		GridPower:     200,   // Import
		IsOffPeak:     false, // HP mode
		MaxCurrent:    40.0,
		MaxHousePower: 12000.0,
		TargetPower:   0.0,
		Timestamp:     time.Now(),
	}

	output := regulator.Calculate(input)

	// Avec import, le PID devrait donner un courant très faible ou nul
	assert.False(t, output.IsCharging)
	assert.Equal(t, "HP", output.DebugInfo["mode"])
}

func TestPIDRegulator_OnPeakWithSurplus(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)

	config := PIDConfig{
		Kp:               0.002,
		Ki:               0.0005,
		Kd:               0.00002,
		SmoothingFactor:  0.1,
		MaxTimeGap:       60.0,
		SurplusThreshold: 100.0,
		ImportThreshold:  50.0,
	}

	regulator := NewPIDRegulator(config, logger)

	// Premier calcul avec surplus
	input := RegulationInput{
		GridPower:     -1500, // Surplus
		IsOffPeak:     false, // HP mode
		MaxCurrent:    40.0,
		MaxHousePower: 12000.0,
		TargetPower:   0.0,
		Timestamp:     time.Now(),
	}

	output := regulator.Calculate(input)

	// Avec surplus, le PID devrait augmenter le courant
	assert.True(t, output.TargetCurrent > 0)
	// Le message peut varier selon l'état du PID
	assert.True(t, output.IsCharging || output.TargetCurrent > 0)
	assert.Equal(t, "HP", output.DebugInfo["mode"])

	// Vérifier que l'état interne est mis à jour
	status := regulator.GetStatus()
	assert.Contains(t, status, "current_target")
	assert.Contains(t, status, "smoothed_power")
}

func TestPIDRegulator_TimeGapReset(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)

	config := PIDConfig{
		Kp:               0.001,
		Ki:               0.0001,
		Kd:               0.00001,
		SmoothingFactor:  0.1,
		MaxTimeGap:       1.0, // 1 seconde pour forcer le reset
		SurplusThreshold: 100.0,
		ImportThreshold:  50.0,
	}

	regulator := NewPIDRegulator(config, logger)

	// Premier calcul
	now := time.Now()
	input1 := RegulationInput{
		GridPower:     -1000,
		IsOffPeak:     false,
		MaxCurrent:    40.0,
		MaxHousePower: 12000.0,
		TargetPower:   0.0,
		Timestamp:     now,
	}

	output1 := regulator.Calculate(input1)
	_ = output1.TargetCurrent // firstTarget non utilisé

	// Deuxième calcul après un gap important
	input2 := RegulationInput{
		GridPower:     -1000,
		IsOffPeak:     false,
		MaxCurrent:    40.0,
		MaxHousePower: 12000.0,
		TargetPower:   0.0,
		Timestamp:     now.Add(5 * time.Second), // > MaxTimeGap
	}

	_ = regulator.Calculate(input2) // output2 non utilisé

	// Le régulateur devrait avoir été reset
	status := regulator.GetStatus()
	resetCount := status["reset_count"].(int64)
	assert.True(t, resetCount > 0)
}

func TestPIDRegulator_Reset(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.FatalLevel)

	config := PIDConfig{
		Kp:               0.001,
		Ki:               0.0001,
		Kd:               0.00001,
		SmoothingFactor:  0.1,
		MaxTimeGap:       60.0,
		SurplusThreshold: 100.0,
		ImportThreshold:  50.0,
	}

	regulator := NewPIDRegulator(config, logger)

	// Calculer quelque chose pour avoir un état interne
	input := RegulationInput{
		GridPower:     -1000,
		IsOffPeak:     false,
		MaxCurrent:    40.0,
		MaxHousePower: 12000.0,
		TargetPower:   0.0,
		Timestamp:     time.Now(),
	}

	regulator.Calculate(input)

	// Reset
	regulator.Reset()

	// Vérifier que l'état a été reset
	status := regulator.GetStatus()
	assert.Equal(t, 0.0, status["current_target"])
	assert.Equal(t, 0.0, status["previous_error"])
	assert.Equal(t, 0.0, status["integral_error"])
	assert.True(t, status["reset_count"].(int64) > 0)
}

func TestPIDRegulator_GetName(t *testing.T) {
	logger := logrus.New()
	config := PIDConfig{}
	regulator := NewPIDRegulator(config, logger)

	assert.Equal(t, "PID Regulator", regulator.GetName())
}

// Test de scénario réaliste : ton exemple
func TestPIDRegulator_RealisticScenario(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	config := PIDConfig{
		Kp:               0.002,
		Ki:               0.0005,
		Kd:               0.00002,
		SmoothingFactor:  0.1,
		MaxTimeGap:       60.0,
		SurplusThreshold: 100.0,
		ImportThreshold:  50.0,
	}

	regulator := NewPIDRegulator(config, logger)
	baseTime := time.Now()

	scenarios := []struct {
		name     string
		power    float64
		time     time.Duration
		expected string
	}{
		{"Import initial", 1200, 0, "no_charge"},
		{"Surplus detecté", -2000, 5 * time.Second, "charge_start"},
		{"Grid remonte", 200, 10 * time.Second, "charge_reduce"},
		{"Equilibre", -100, 15 * time.Second, "charge_adjust"},
	}

	var lastCurrent float64

	for _, scenario := range scenarios {
		input := RegulationInput{
			GridPower:     scenario.power,
			IsOffPeak:     false, // Mode HP
			MaxCurrent:    40.0,
			MaxHousePower: 12000.0,
			TargetPower:   0.0,
			Timestamp:     baseTime.Add(scenario.time),
		}

		output := regulator.Calculate(input)

		switch scenario.expected {
		case "no_charge":
			assert.False(t, output.IsCharging, "Scenario %s: should not charge", scenario.name)
		case "charge_start":
			assert.True(t, output.IsCharging, "Scenario %s: should start charging", scenario.name)
			assert.True(t, output.TargetCurrent > lastCurrent, "Scenario %s: current should increase", scenario.name)
		case "charge_reduce":
			assert.True(t, output.TargetCurrent < lastCurrent, "Scenario %s: current should decrease", scenario.name)
		case "charge_adjust":
			// Le courant devrait s'ajuster finement
			assert.True(t, output.IsCharging, "Scenario %s: should still charge", scenario.name)
		}

		lastCurrent = output.TargetCurrent
		t.Logf("Scenario %s: Power=%.0fW, Current=%.1fA, Reason=%s",
			scenario.name, scenario.power, output.TargetCurrent, output.Reason)
	}
}
