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

	// Créer le régulateur OpenEVSE
	openevseConfig := regulation.OpenEVSEConfig{
		ReservePowerW:    100.0,  // 100W de réserve pour éviter l'import
		HysteresisPowerW: 600.0,  // 600W d'hystérésis comme dans l'article
		MinChargeTimeS:   60.0,   // 1 minute minimum (réduit pour test interactif)
		SmoothingAttackS: 15.0,   // 15s pour attaque (réduit pour test)
		SmoothingDecayS:  45.0,   // 45s pour décroissance (réduit pour test)
		MinChargePowerW:  1400.0, // 1.4kW minimum pour démarrer (6A)
		PollIntervalS:    5.0,    // 5s pour test interactif
		MaxDeltaPerStepA: 3.0,    // Max 3A de variation par étape
	}
	regulator := regulation.NewOpenEVSERegulator(openevseConfig, logger)

	fmt.Println("📋 Configuration OpenEVSE:")
	fmt.Printf("   Réserve: %.0fW, Hystérésis: %.0fW\n", openevseConfig.ReservePowerW, openevseConfig.HysteresisPowerW)
	fmt.Printf("   Temps min charge: %.0fs, Puissance min: %.0fW\n", openevseConfig.MinChargeTimeS, openevseConfig.MinChargePowerW)
	fmt.Printf("   Lissage attaque/décroissance: %.0fs/%.0fs\n", openevseConfig.SmoothingAttackS, openevseConfig.SmoothingDecayS)
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
	fmt.Println("   <grid_power>        - Entrer une puissance grid (W) (ex: -2500, 1000)")
	fmt.Println("   <grid_power> <amps> - Grid + courant actuel (ex: 2000 3, -1500 0)")
	fmt.Println("   hc           - Passer en mode HC (heures creuses)")
	fmt.Println("   hp           - Passer en mode HP (heures pleines)")
	fmt.Println("   reset        - Reset du régulateur")
	fmt.Println("   status       - Afficher l'état du régulateur")
	fmt.Println("   config       - Modifier la configuration")
	fmt.Println("   scenario     - Lancer un scénario OpenEVSE")
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
			ovse_divert_showHelp()

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
			ovse_divert_showStatus(regulator)

		case input == "config":
			ovse_divert_updateConfig(&config, regulator, logger)

		case input == "scenario":
			ovse_divert_runScenario(regulator, &stepCount, baseTime, mode, maxCurrent, maxHousePower, &currentCharging)

		default:
			// Essayer de parser comme "grid_power" ou "grid_power current_charging"
			parts := strings.Fields(input)
			if len(parts) == 1 {
				// Format simple: juste la puissance grid
				if power, err := strconv.ParseFloat(parts[0], 64); err == nil {
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

					// Simuler l'application du delta (comme le ChargingManager)
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
						// Mode HC: utiliser directement TargetCurrent pour compatibilité
						currentCharging = output.TargetCurrent
					}

					// Afficher le résultat
					ovse_divert_showOutput(power, output, stepCount, currentCharging)
				} else {
					fmt.Println("❌ Commande inconnue. Tapez 'help' pour voir les commandes.")
				}
			} else if len(parts) == 2 {
				// Format avec courant: "grid_power current_charging"
				if power, err1 := strconv.ParseFloat(parts[0], 64); err1 == nil {
					if charging, err2 := strconv.ParseFloat(parts[1], 64); err2 == nil {
						stepCount++
						timestamp := baseTime.Add(time.Duration(stepCount*5) * time.Second)

						// Forcer le courant spécifié
						currentCharging = charging

						fmt.Printf("🔧 Courant forcé à %.1fA\n", currentCharging)

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

						// NE PAS appliquer le delta automatiquement dans ce mode
						// L'utilisateur contrôle le courant manuellement

						// Afficher le résultat
						ovse_divert_showOutput(power, output, stepCount, currentCharging)
					} else {
						fmt.Println("❌ Courant invalide. Format: 'grid_power current_charging'")
					}
				} else {
					fmt.Println("❌ Puissance grid invalide. Format: 'grid_power current_charging'")
				}
			} else {
				fmt.Println("❌ Commande inconnue. Tapez 'help' pour voir les commandes.")
			}
		}
	}
}

func ovse_divert_showOutput(gridPower float64, output regulation.RegulationOutput, step int, actualCharging float64) {
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

	// Afficher les infos spécifiques OpenEVSE
	if debugInfo, ok := output.DebugInfo["smoothed_excess"]; ok {
		fmt.Printf("   🌞 Surplus lissé:  %8.0f W", debugInfo)
		if val, exists := output.DebugInfo["is_charging"]; exists {
			if isCharging, ok := val.(bool); ok && isCharging {
				if timeInfo, ok2 := output.DebugInfo["time_since_start"]; ok2 {
					fmt.Printf(" | Charge depuis: %.0fs", timeInfo)
				}
			}
		}
		fmt.Println()
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

func ovse_divert_showStatus(regulator regulation.RegulationService) {
	status := regulator.GetStatus()

	fmt.Println("📊 État du régulateur:")
	fmt.Printf("   Nom:                %s\n", status["name"])

	if smoothedExcess, ok := status["smoothed_excess_power"]; ok {
		fmt.Printf("   Surplus lissé:      %.0fW\n", smoothedExcess)
	}
	if isCharging, ok := status["is_charging"]; ok {
		fmt.Printf("   En charge:          %v\n", isCharging)
	}
	if activations, ok := status["activation_count"]; ok {
		fmt.Printf("   Activations:        %v\n", activations)
	}
	if deactivations, ok := status["deactivation_count"]; ok {
		fmt.Printf("   Désactivations:     %v\n", deactivations)
	}
	if startTime, ok := status["charging_start_time"]; ok {
		if t, ok := startTime.(time.Time); ok && !t.IsZero() {
			fmt.Printf("   Début charge:       %s\n", t.Format("15:04:05"))
		}
	}

	if config, ok := status["config"]; ok {
		if openevseConfig, ok := config.(regulation.OpenEVSEConfig); ok {
			fmt.Println("   Configuration:")
			fmt.Printf("     Réserve: %.0fW, Hystérésis: %.0fW\n", openevseConfig.ReservePowerW, openevseConfig.HysteresisPowerW)
			fmt.Printf("     Temps min: %.0fs, Puissance min: %.0fW\n", openevseConfig.MinChargeTimeS, openevseConfig.MinChargePowerW)
		}
	}
}

func ovse_divert_showHelp() {
	fmt.Println("🎮 Guide d'utilisation - Régulateur OpenEVSE:")
	fmt.Println()
	fmt.Println("📝 Formats d'entrée:")
	fmt.Println("   -2500      → Surplus de 2500W, courant auto-ajusté")
	fmt.Println("   1000       → Import de 1000W, courant auto-ajusté")
	fmt.Println("   2000 3     → Import 2000W avec 3A en cours de charge")
	fmt.Println("   -1500 0    → Surplus 1500W sans charge actuelle")
	fmt.Println()
	fmt.Println("⚙️  Contrôles:")
	fmt.Println("   hp/hc    → Changer de mode tarifaire")
	fmt.Println("   reset    → Remettre le régulateur à zéro")
	fmt.Println("   status   → Voir l'état interne du régulateur")
	fmt.Println("   scenario → Lancer un scénario OpenEVSE")
	fmt.Println()
	fmt.Println("💡 Comportement OpenEVSE:")
	fmt.Println("   • Seuil démarrage: 1400W + 600W (hystérésis) = 2000W")
	fmt.Println("   • Temps minimum: 60s de charge obligatoire")
	fmt.Println("   • Lissage: Attaque 15s / Décroissance 45s")
	fmt.Println("   • Réserve: 100W pour éviter l'import")
	fmt.Println()
	fmt.Println("🧪 Tests de régulation:")
	fmt.Println("   1. 'reset' pour partir de zéro")
	fmt.Println("   2. '1000 0' → Import avec 0A, vérifier pas de charge")
	fmt.Println("   3. '-2500 0' → Surplus avec 0A, vérifier démarrage")
	fmt.Println("   4. '-1000 10' → Surplus faible avec charge, vérifier maintien")
	fmt.Println("   5. '500 15' → Import avec charge, vérifier arrêt après temps min")
	fmt.Println()
	fmt.Println("💡 Avantage format 'power amps':")
	fmt.Println("   → Teste la vraie logique OpenEVSE avec feedback réel")
	fmt.Println("   → Vérifie l'hystérésis et le temps minimum de charge")
}

func ovse_divert_updateConfig(config *regulation.PIDConfig, regulator *regulation.OpenEVSERegulator, logger *logrus.Logger) {
	fmt.Println("⚙️ Configuration OpenEVSE actuelle:")
	status := regulator.GetStatus()
	if configData, ok := status["config"]; ok {
		if openevseConfig, ok := configData.(regulation.OpenEVSEConfig); ok {
			fmt.Printf("   Réserve: %.0fW\n", openevseConfig.ReservePowerW)
			fmt.Printf("   Hystérésis: %.0fW\n", openevseConfig.HysteresisPowerW)
			fmt.Printf("   Temps min charge: %.0fs\n", openevseConfig.MinChargeTimeS)
			fmt.Printf("   Puissance min: %.0fW\n", openevseConfig.MinChargePowerW)
		}
	}
	fmt.Println()
	fmt.Println("📝 Entrez les nouveaux paramètres (ou Entrée pour garder):")

	scanner := bufio.NewScanner(os.Stdin)

	// Pour l'instant, on ne permet que de modifier quelques paramètres clés
	newConfig := regulation.OpenEVSEConfig{
		ReservePowerW:    100.0,
		HysteresisPowerW: 200.0,
		MinChargeTimeS:   60.0,
		SmoothingAttackS: 15.0,
		SmoothingDecayS:  45.0,
		MinChargePowerW:  1400.0,
		PollIntervalS:    5.0,
		MaxDeltaPerStepA: 3.0,
	}

	fmt.Print("   Hystérésis (W): ")
	if scanner.Scan() && scanner.Text() != "" {
		if val, err := strconv.ParseFloat(scanner.Text(), 64); err == nil {
			newConfig.HysteresisPowerW = val
		}
	}

	fmt.Print("   Temps min charge (s): ")
	if scanner.Scan() && scanner.Text() != "" {
		if val, err := strconv.ParseFloat(scanner.Text(), 64); err == nil {
			newConfig.MinChargeTimeS = val
		}
	}

	fmt.Print("   Puissance min démarrage (W): ")
	if scanner.Scan() && scanner.Text() != "" {
		if val, err := strconv.ParseFloat(scanner.Text(), 64); err == nil {
			newConfig.MinChargePowerW = val
		}
	}

	// Recreer le régulateur avec la nouvelle config
	*regulator = *regulation.NewOpenEVSERegulator(newConfig, logger)
	fmt.Println("✅ Configuration mise à jour et régulateur OpenEVSE reset")
}

func ovse_divert_runScenario(regulator regulation.RegulationService, stepCount *int, baseTime time.Time, mode string, maxCurrent, maxHousePower float64, currentCharging *float64) {
	fmt.Println("🎬 Lancement du scénario OpenEVSE: Hystérésis et temps minimum")
	fmt.Println()

	scenarios := []struct {
		name  string
		power float64
		delay int
	}{
		{"Import initial", 1500, 0},
		{"Surplus faible", -1200, 10},
		{"Surplus suffisant", -2500, 10},
		{"Surplus important", -4000, 10},
		{"Surplus diminue", -1000, 10},
		{"Import léger", 500, 20},
		{"Nouveau surplus", -2000, 10},
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
		ovse_divert_showOutput(scenario.power, output, *stepCount, *currentCharging)
		fmt.Println()
	}

	fmt.Println("✅ Scénario terminé! Tu peux continuer à tester manuellement.")
}
