package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type TestScenario struct {
	Name        string
	HPHCState   string // "HP" or "HC"
	GridPowers  []float64
	Intervals   []int // seconds between each power value
	Description string
}

type GridPowerMessage struct {
	Power     float64   `json:"power"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	// MQTT Configuration
	broker := "tcp://localhost:1883"
	gridTopic := "energy/grid/power"
	hphcTopic := "energy/tariff/state"

	// Connect to MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("mqtt-test-simulator")
	opts.SetAutoReconnect(true)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", token.Error())
	}
	defer client.Disconnect(250)

	fmt.Println("🚀 MQTT Simulator connected to", broker)
	fmt.Println("📡 Publishing to topics:")
	fmt.Println("   - Grid Power:", gridTopic)
	fmt.Println("   - HP/HC State:", hphcTopic)
	fmt.Println()

	// Test scenarios
	scenarios := []TestScenario{
		{
			Name:        "Test 1: Surplus solaire en HP",
			HPHCState:   "HP",
			GridPowers:  []float64{1200, -2000, 200, -100, 50, -300, 0, -50},
			Intervals:   []int{5, 10, 5, 5, 5, 5, 5, 10},
			Description: "Surplus puis consommation, vérification régulation PID",
		},
		{
			Name:        "Test 2: Mode HC - Charge max",
			HPHCState:   "HC",
			GridPowers:  []float64{500, 8000, 12500, 6000},
			Intervals:   []int{5, 10, 10, 10},
			Description: "Mode heures creuses - charge limitée par 12kW",
		},
		{
			Name:        "Test 3: Variations rapides",
			HPHCState:   "HP",
			GridPowers:  []float64{-1000, 300, -800, 150, -500, 400, -200},
			Intervals:   []int{3, 3, 3, 3, 3, 3, 3},
			Description: "Variations rapides pour tester la stabilité du PID",
		},
		{
			Name:        "Test 4: Grosse production solaire",
			HPHCState:   "HP",
			GridPowers:  []float64{-5000, -8000, -6000, -2000, 0, 1000},
			Intervals:   []int{10, 10, 10, 10, 5, 5},
			Description: "Production solaire importante - test des limites 40A",
		},
	}

	// Run all scenarios
	for i, scenario := range scenarios {
		runScenario(client, scenario, gridTopic, hphcTopic)

		if i < len(scenarios)-1 {
			fmt.Println("\n⏸️  Pause 10s avant le prochain test...")
			time.Sleep(10 * time.Second)
		}
	}

	fmt.Println("\n✅ Tous les tests terminés!")
	fmt.Println("📊 Vérifiez les logs du serveur OCPP pour analyser le comportement du PID")
}

func runScenario(client mqtt.Client, scenario TestScenario, gridTopic, hphcTopic string) {
	fmt.Printf("🧪 %s\n", scenario.Name)
	fmt.Printf("📝 %s\n", scenario.Description)
	fmt.Printf("⚡ Mode: %s\n", scenario.HPHCState)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Set HP/HC state
	publishHPHC(client, hphcTopic, scenario.HPHCState)
	time.Sleep(2 * time.Second)

	// Publish grid power values
	for i, power := range scenario.GridPowers {
		publishGridPower(client, gridTopic, power)

		interval := 5 // default
		if i < len(scenario.Intervals) {
			interval = scenario.Intervals[i]
		}

		fmt.Printf("📈 T+%ds: Grid = %+.0fW", getTotalTime(scenario.Intervals, i), power)
		if power < 0 {
			fmt.Printf(" (surplus de %.0fW)", -power)
		} else if power > 0 {
			fmt.Printf(" (import de %.0fW)", power)
		} else {
			fmt.Printf(" (équilibre)")
		}
		fmt.Println()

		if i < len(scenario.GridPowers)-1 {
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("✅ %s terminé\n", scenario.Name)
}

func publishGridPower(client mqtt.Client, topic string, power float64) {
	message := GridPowerMessage{
		Power:     power,
		Timestamp: time.Now(),
	}

	payload, _ := json.Marshal(message)

	token := client.Publish(topic, 1, false, payload)
	token.Wait()
}

func publishHPHC(client mqtt.Client, topic string, state string) {
	token := client.Publish(topic, 1, false, state)
	token.Wait()
	fmt.Printf("🔄 Mode tarifaire: %s\n", state)
}

func getTotalTime(intervals []int, currentIndex int) int {
	total := 0
	for i := 0; i < currentIndex && i < len(intervals); i++ {
		total += intervals[i]
	}
	return total
}

// Bonus: Interactive mode
func interactiveMode(client mqtt.Client, gridTopic, hphcTopic string) {
	fmt.Println("\n🎮 Mode interactif activé!")
	fmt.Println("Commandes:")
	fmt.Println("  hp/hc <valeur>  - Changer le mode tarifaire")
	fmt.Println("  grid <valeur>   - Définir la puissance réseau")
	fmt.Println("  quit           - Quitter")

	for {
		fmt.Print("\n> ")
		var cmd string
		var value string
		fmt.Scanln(&cmd, &value)

		switch cmd {
		case "hp", "hc":
			publishHPHC(client, hphcTopic, cmd)
		case "grid":
			var power float64
			fmt.Sscanf(value, "%f", &power)
			publishGridPower(client, gridTopic, power)
			fmt.Printf("📡 Grid power: %.0fW\n", power)
		case "quit", "exit":
			return
		default:
			fmt.Println("❌ Commande inconnue")
		}
	}
}
