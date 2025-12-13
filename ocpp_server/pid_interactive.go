package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"ocpp-server/internal/regulation"

	"github.com/sirupsen/logrus"
)

func main() {
	fmt.Println("🧪 PID Regulator Interactive Tester")
	fmt.Println("====================================")
	fmt.Println()

	// Configuration du logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Configuration du PID - optimisée pour convergence rapide
	config := regulation.PIDConfig{
		Kp:               0.035, // Gain proportionnel très agressif
		Ki:               0.020, // Gain intégral très agressif
		Kd:               0.005, // Gain dérivé très agressif
		SmoothingFactor:  0.5,   // Lissage rapide pour réactivité
		MaxTimeGap:       60.0,  // Reset si gap > 60s
		SurplusThreshold: 200.0, // 200W surplus min (plus stable)
		ImportThreshold:  100.0, // 100W import max (plus stable)
	}

	// Créer le nouveau régulateur Delta
	deltaConfig := regulation.DeltaPIDConfig{
		Kp:               config.Kp,
		Ki:               config.Ki,
		Kd:               config.Kd,
		SmoothingFactor:  config.SmoothingFactor,
		MaxTimeGap:       config.MaxTimeGap,
		SurplusThreshold: config.SurplusThreshold,
		ImportThreshold:  config.ImportThreshold,
		MaxDeltaPerStep:  5.0, // Max 5A de variation par étape
	}
	regulator := regulation.NewDeltaRegulator(deltaConfig, logger)

	fmt.Println("📋 Configuration PID:")
	fmt.Printf("   Kp=%.4f, Ki=%.6f, Kd=%.6f\n", config.Kp, config.Ki, config.Kd)
	fmt.Printf("   Seuil surplus: %.0fW, Seuil import: %.0fW\n", config.SurplusThreshold, config.ImportThreshold)
	fmt.Println()

	// Variables pour la session
	var stepCount int
	baseTime := time.Now()
	scanner := bufio.NewScanner(os.Stdin)

	// État de simulation
	mode := "HP" // HP par défaut
	maxCurrent := 40.0
	maxHousePower := 12000.0
	currentCharging := 0.0 // Simulation du courant actuellement en charge

	fmt.Println("🎮 Commandes disponibles:")
	fmt.Println("   <nombre>     - Entrer une puissance grid (W) (ex: -1500, 200)")
	fmt.Println("   hc           - Passer en mode HC (heures creuses)")
	fmt.Println("   hp           - Passer en mode HP (heures pleines)")
	fmt.Println("   reset        - Reset du PID")
	fmt.Println("   status       - Afficher l'état du PID")
	fmt.Println("   config       - Modifier la configuration")
	fmt.Println("   scenario     - Lancer un scénario prédéfini")
	fmt.Println("   help         - Afficher cette aide")
	fmt.Println("   quit         - Quitter")
	fmt.Println()

	for {
		// Affichage du prompt avec état actuel
		fmt.Printf("\n[Step %d | Mode: %s | Charging: %.1fA] > ", stepCount, mode, currentCharging)

		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch {
		case input == "quit" || input == "q":
			fmt.Println("👋 Au revoir!")
			return

		case input == "help" || input == "h":
			showHelp()

		case input == "reset":
			regulator.Reset()
			fmt.Println("🔄 PID reset effectué")

		case input == "hp":
			mode = "HP"
			fmt.Println("⚡ Mode HP (heures pleines) activé")

		case input == "hc":
			mode = "HC"
			fmt.Println("🌙 Mode HC (heures creuses) activé")

		case input == "status":
			showStatus(regulator)

		case input == "config":
			updateConfig(&config, regulator, logger)

		case input == "scenario":
			runScenario(regulator, &stepCount, baseTime, mode, maxCurrent, maxHousePower, &currentCharging)

		default:
			// Essayer de parser comme une puissance
			if power, err := strconv.ParseFloat(input, 64); err == nil {
				stepCount++
				timestamp := baseTime.Add(time.Duration(stepCount*5) * time.Second)

				// Préparer l'input pour le régulateur
				regulationInput := regulation.RegulationInput{
					GridPower:       power,
					CurrentCharging: currentCharging,
					IsOffPeak:       (mode == "HC"),
					MaxCurrent:      maxCurrent,
					MaxHousePower:   maxHousePower,
					TargetPower:     0.0, // Consigne = 0W
					Timestamp:       timestamp,
				}

				// Calculer la régulation
				output := regulator.Calculate(regulationInput)

				// Simuler l'application du delta
				if output.DeltaCurrent != 0 {
					newCharging := currentCharging + output.DeltaCurrent
					// Appliquer les contraintes de courant minimum
					if newCharging < 6.0 && newCharging > 0 {
						newCharging = 0 // Trop faible pour charger
					}
					if newCharging < 0 {
						newCharging = 0
					}
					if newCharging > maxCurrent {
						newCharging = maxCurrent
					}
					currentCharging = newCharging
				} else if mode == "HC" {
					// Mode HC: utiliser directement TargetCurrent
					currentCharging = output.TargetCurrent
				}

				// Afficher le résultat
				showOutput(power, output, stepCount, currentCharging)
			} else {
				fmt.Println("❌ Commande inconnue. Tapez 'help' pour voir les commandes.")
			}
		}
	}
}

func showOutput(gridPower float64, output regulation.RegulationOutput, step int, actualCharging float64) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("📊 Step %d - Résultat de la régulation\n", step)
	fmt.Printf("   🔌 Grid Power:     %+8.1f W", gridPower)
	if gridPower < 0 {
		fmt.Printf(" (surplus de %.0fW)\n", -gridPower)
	} else if gridPower > 0 {
		fmt.Printf(" (import de %.0fW)\n", gridPower)
	} else {
		fmt.Printf(" (équilibre)\n")
	}

	fmt.Printf("   ⚡ Courant cible:  %8.2f A", output.TargetCurrent)
	if output.ShouldCharge {
		fmt.Printf(" ✅ CHARGE\n")
	} else {
		fmt.Printf(" ❌ Arrêt\n")
	}

	// Afficher le delta si disponible (nouveau régulateur)
	if output.DeltaCurrent != 0 {
		fmt.Printf("   📊 Delta courant:  %+8.2f A", output.DeltaCurrent)
		if output.DeltaCurrent > 0 {
			fmt.Printf(" ⬆️ Augmentation\n")
		} else {
			fmt.Printf(" ⬇️ Réduction\n")
		}
	}

	// Afficher le courant réellement appliqué
	fmt.Printf("   ⚡ Courant réel:   %8.2f A", actualCharging)
	if actualCharging > 0 {
		fmt.Printf(" ✅ EN CHARGE\n")
	} else {
		fmt.Printf(" ❌ Arrêté\n")
	}

	fmt.Printf("   📝 Raison:         %s\n", output.Reason)

	// Debug info détaillé
	if debugInfo, ok := output.DebugInfo["smoothed_power"]; ok {
		fmt.Printf("   📈 Puissance lissée: %6.1f W\n", debugInfo)
	}
	if debugInfo, ok := output.DebugInfo["error"]; ok {
		fmt.Printf("   📉 Erreur PID:      %+6.1f W\n", debugInfo)
	}
	if debugInfo, ok := output.DebugInfo["dt"]; ok {
		fmt.Printf("   ⏱️  Delta temps:    %6.1f s\n", debugInfo)
	}
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func showStatus(regulator regulation.RegulationService) {
	status := regulator.GetStatus()

	fmt.Println("📊 État du régulateur PID:")
	fmt.Printf("   Nom:              %s\n", status["name"])
	fmt.Printf("   Courant cible:    %.2fA\n", status["current_target"])
	fmt.Printf("   Erreur précédente: %.1fW\n", status["previous_error"])
	fmt.Printf("   Erreur intégrale:  %.1f\n", status["integral_error"])
	fmt.Printf("   Puissance lissée:  %.1fW\n", status["smoothed_power"])
	fmt.Printf("   Resets:           %d\n", status["reset_count"])

	if config, ok := status["config"]; ok {
		if pidConfig, ok := config.(regulation.PIDConfig); ok {
			fmt.Println("   Configuration:")
			fmt.Printf("     Kp: %.6f, Ki: %.6f, Kd: %.6f\n", pidConfig.Kp, pidConfig.Ki, pidConfig.Kd)
		}
	}
}

func showHelp() {
	fmt.Println("🎮 Guide d'utilisation:")
	fmt.Println()
	fmt.Println("📝 Entrer des valeurs de puissance:")
	fmt.Println("   -2000    → Surplus de 2000W (panneaux solaires)")
	fmt.Println("   200      → Import de 200W du réseau")
	fmt.Println("   0        → Équilibre parfait")
	fmt.Println()
	fmt.Println("⚙️  Contrôles:")
	fmt.Println("   hp/hc    → Changer de mode tarifaire")
	fmt.Println("   reset    → Remettre le PID à zéro")
	fmt.Println("   status   → Voir l'état interne du PID")
	fmt.Println("   scenario → Lancer ton exemple (1200→-2000→200→-100)")
	fmt.Println()
	fmt.Println("💡 Exemples d'utilisation:")
	fmt.Println("   1. Tape 'hp' pour mode HP")
	fmt.Println("   2. Tape '-1500' pour simuler 1500W de surplus")
	fmt.Println("   3. Observe le courant calculé")
	fmt.Println("   4. Tape '300' pour simuler 300W d'import")
	fmt.Println("   5. Vois comme le PID s'adapte!")
}

func updateConfig(config *regulation.PIDConfig, regulator *regulation.DeltaRegulator, logger *logrus.Logger) {
	fmt.Println("⚙️ Configuration actuelle:")
	fmt.Printf("   Kp: %.6f\n", config.Kp)
	fmt.Printf("   Ki: %.6f\n", config.Ki)
	fmt.Printf("   Kd: %.6f\n", config.Kd)
	fmt.Println()
	fmt.Println("📝 Entrez les nouveaux gains (ou Entrée pour garder):")

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Print("   Kp: ")
	if scanner.Scan() && scanner.Text() != "" {
		if kp, err := strconv.ParseFloat(scanner.Text(), 64); err == nil {
			config.Kp = kp
		}
	}

	fmt.Print("   Ki: ")
	if scanner.Scan() && scanner.Text() != "" {
		if ki, err := strconv.ParseFloat(scanner.Text(), 64); err == nil {
			config.Ki = ki
		}
	}

	fmt.Print("   Kd: ")
	if scanner.Scan() && scanner.Text() != "" {
		if kd, err := strconv.ParseFloat(scanner.Text(), 64); err == nil {
			config.Kd = kd
		}
	}

	// Recreer le régulateur avec la nouvelle config
	deltaConfig := regulation.DeltaPIDConfig{
		Kp:               config.Kp,
		Ki:               config.Ki,
		Kd:               config.Kd,
		SmoothingFactor:  config.SmoothingFactor,
		MaxTimeGap:       config.MaxTimeGap,
		SurplusThreshold: config.SurplusThreshold,
		ImportThreshold:  config.ImportThreshold,
		MaxDeltaPerStep:  5.0,
	}
	*regulator = *regulation.NewDeltaRegulator(deltaConfig, logger)
	fmt.Println("✅ Configuration mise à jour et Delta PID reset")
}

func runScenario(regulator regulation.RegulationService, stepCount *int, baseTime time.Time, mode string, maxCurrent, maxHousePower float64, currentCharging *float64) {
	fmt.Println("🎬 Lancement du scénario: ton exemple (1200W → -2000W → 200W → -100W)")
	fmt.Println()

	scenarios := []struct {
		name  string
		power float64
		delay int
	}{
		{"Import initial", 1200, 0},
		{"Surplus solaire", -2000, 5},
		{"Grid remonte", 200, 5},
		{"Petit surplus", -100, 5},
	}

	for i, scenario := range scenarios {
		*stepCount++
		timestamp := baseTime.Add(time.Duration(*stepCount*scenario.delay) * time.Second)

		input := regulation.RegulationInput{
			GridPower:       scenario.power,
			CurrentCharging: *currentCharging,
			IsOffPeak:       (mode == "HC"),
			MaxCurrent:      maxCurrent,
			MaxHousePower:   maxHousePower,
			TargetPower:     0.0,
			Timestamp:       timestamp,
		}

		output := regulator.Calculate(input)

		// Simuler l'application du delta
		if output.DeltaCurrent != 0 {
			newCharging := *currentCharging + output.DeltaCurrent
			if newCharging < 6.0 && newCharging > 0 {
				newCharging = 0
			}
			if newCharging < 0 {
				newCharging = 0
			}
			if newCharging > maxCurrent {
				newCharging = maxCurrent
			}
			*currentCharging = newCharging
		}

		fmt.Printf("🎬 Scénario %d: %s\n", i+1, scenario.name)
		showOutput(scenario.power, output, *stepCount, *currentCharging)
		fmt.Println()
	}

	fmt.Println("✅ Scénario terminé! Tu peux continuer à tester manuellement.")
}
