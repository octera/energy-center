package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Simple manual test without docker dependency
func main() {
	fmt.Println("🧪 Test Manuel OCPP - Simulation MQTT")
	fmt.Println("=====================================")
	fmt.Println()

	// Configuration MQTT
	broker := "tcp://localhost:1883"
	gridTopic := "energy/grid/power"
	hphcTopic := "energy/tariff/state"

	// Connexion MQTT
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("manual-test")

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("❌ Impossible de se connecter à MQTT (%s)", broker)
		log.Printf("💡 Assurez-vous que le broker MQTT fonctionne:")
		log.Printf("   docker run -it -p 1883:1883 eclipse-mosquitto:2.0")
		log.Printf("Erreur: %s", token.Error().Error())
		return
	}
	defer client.Disconnect(250)

	fmt.Printf("✅ Connecté au broker MQTT: %s\n", broker)
	fmt.Println()

	// Test Scenario: Ton exemple exact
	fmt.Println("🎯 Test du Scénario PID")
	fmt.Println("Objectif: Vérifier la régulation autour de 0W en mode HP")
	fmt.Println()

	scenarios := []struct {
		step     string
		hphc     string
		grid     float64
		wait     int
		expected string
	}{
		{"1. Initialisation", "HP", 1200, 3, "Pas de charge (grid > 0)"},
		{"2. Surplus solaire", "HP", -2000, 10, "PID commence à charger"},
		{"3. Grid remonte", "HP", 200, 10, "PID réduit la charge"},
		{"4. Nouvel équilibre", "HP", -100, 10, "PID ajuste finement"},
		{"5. Stabilisation", "HP", 50, 10, "PID maintient l'équilibre"},
		{"6. Test HC", "HC", 1000, 10, "Charge max (mode HC)"},
	}

	for _, scenario := range scenarios {
		fmt.Printf("📊 %s\n", scenario.step)
		fmt.Printf("   Mode: %s | Grid: %+.0fW\n", scenario.hphc, scenario.grid)
		fmt.Printf("   Attendu: %s\n", scenario.expected)

		// Publier HP/HC
		publishSimple(client, hphcTopic, scenario.hphc)
		time.Sleep(1 * time.Second)

		// Publier Grid Power
		publishGridPower(client, gridTopic, scenario.grid)

		fmt.Printf("   ⏳ Attente %ds pour observer la réaction...\n", scenario.wait)
		time.Sleep(time.Duration(scenario.wait) * time.Second)
		fmt.Println()
	}

	fmt.Println("✅ Scénario de test terminé!")
	fmt.Println()
	fmt.Println("📋 Pour analyser les résultats, vérifiez les logs du serveur OCPP:")
	fmt.Println("   - Recherchez 'PID:' pour voir la régulation")
	fmt.Println("   - Recherchez 'Allocated' pour voir la distribution du courant")
	fmt.Println("   - Recherchez 'Grid power updated' pour voir les mesures reçues")

	// Mode interactif optionnel
	fmt.Println()
	fmt.Print("🎮 Voulez-vous passer en mode interactif ? (y/N): ")
	var response string
	fmt.Scanln(&response)

	if response == "y" || response == "Y" {
		interactiveMode(client, gridTopic, hphcTopic)
	}
}

func publishGridPower(client mqtt.Client, topic string, power float64) {
	message := map[string]interface{}{
		"power":     power,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	payload, _ := json.Marshal(message)

	token := client.Publish(topic, 1, false, payload)
	token.Wait()

	fmt.Printf("📡 Publié: Grid Power = %.0fW\n", power)
}

func publishSimple(client mqtt.Client, topic string, value string) {
	token := client.Publish(topic, 1, false, value)
	token.Wait()

	fmt.Printf("📡 Publié: %s = %s\n", topic, value)
}

func interactiveMode(client mqtt.Client, gridTopic, hphcTopic string) {
	fmt.Println()
	fmt.Println("🎮 Mode Interactif Activé")
	fmt.Println("========================")
	fmt.Println("Commandes disponibles:")
	fmt.Println("  hp          - Passer en mode HP (heures pleines)")
	fmt.Println("  hc          - Passer en mode HC (heures creuses)")
	fmt.Println("  grid <watts> - Définir la puissance réseau")
	fmt.Println("  Examples prédefinis:")
	fmt.Println("    surplus    - Simuler un surplus de 1500W")
	fmt.Println("    import     - Simuler un import de 500W")
	fmt.Println("    equilibre  - Simuler l'équilibre (0W)")
	fmt.Println("  quit       - Quitter")
	fmt.Println()

	for {
		fmt.Print("🎮 > ")
		var cmd string
		fmt.Scanln(&cmd)

		switch cmd {
		case "hp":
			publishSimple(client, hphcTopic, "HP")

		case "hc":
			publishSimple(client, hphcTopic, "HC")

		case "surplus":
			publishGridPower(client, gridTopic, -1500)

		case "import":
			publishGridPower(client, gridTopic, 500)

		case "equilibre":
			publishGridPower(client, gridTopic, 0)

		case "grid":
			fmt.Print("   Puissance (W): ")
			var power float64
			fmt.Scanln(&power)
			publishGridPower(client, gridTopic, power)

		case "quit", "exit", "q":
			fmt.Println("👋 Au revoir!")
			return

		case "help", "h":
			fmt.Println("📖 Tapez une des commandes listées ci-dessus")

		default:
			// Essayer de parser comme une puissance directe
			var power float64
			if n, _ := fmt.Sscanf(cmd, "%f", &power); n == 1 {
				publishGridPower(client, gridTopic, power)
			} else {
				fmt.Println("❌ Commande inconnue. Tapez 'help' pour voir les commandes")
			}
		}
	}
}
