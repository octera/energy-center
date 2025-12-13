# 🔌 Serveur OCPP - Gestion Intelligente de Charge

## 🎯 Fonctionnalités

- **Serveur OCPP WebSocket** pour 2 bornes de recharge
- **Régulation PID** pour autoconsommation solaire optimale  
- **Gestion HP/HC** avec priorités configurables
- **Limitation totale 40A** (monophasé)
- **Asservissement en temps réel** sur la puissance réseau

## 🚀 Démarrage Rapide

### 1. Configuration
```bash
cp config.yaml.example config.yaml
# Éditer config.yaml avec vos topics MQTT
```

### 2. Lancement avec Docker
```bash
docker-compose up -d
```

### 3. Build manuel
```bash
go build -o ocpp-server ./cmd
./ocpp-server
```

## 🧪 Tests et Validation

### Test Automatique Complet
```bash
./run_tests.sh
```

### Test Manuel Simple
```bash
# Option 1: Script automatique
./quick_test.sh

# Option 2: Manuel
# 1. Démarrer un broker MQTT
docker run -d -p 1883:1883 --name mqtt eclipse-mosquitto:2.0

# 2. Démarrer le serveur OCPP
go run ./cmd &

# 3. Lancer le simulateur
go run mqtt_simulator.go
```

### Test Interactif
```bash
go run mqtt_simulator.go
# Puis utiliser les commandes:
# hp/hc - changer le mode
# surplus/import/equilibre - scénarios prédéfinis  
# grid 1500 - définir puissance custom
```

## ⚙️ Configuration PID

Paramètres de régulation dans `config.yaml`:

```yaml
charging:
  # Régulateur PID (mode HP uniquement)
  pid_kp: 0.001      # Gain proportionnel (réactivité)
  pid_ki: 0.0001     # Gain intégral (précision)  
  pid_kd: 0.00001    # Gain dérivé (stabilité)
  grid_target_power: 0.0  # Consigne = 0W
```

## 📊 Algorithme de Régulation

### Mode HP (Heures Pleines)
```
Objectif: grid_power ≈ 0W (autoconsommation)

1. Mesure: grid_power via MQTT
2. Erreur: error = 0 - grid_power  
3. PID: ajustement = Kp*error + Ki*∫error + Kd*d(error)/dt
4. Update: current_target += ajustement/230V
5. Distribution: priorité station1 > station2
```

### Mode HC (Heures Creuses)
```
Charge maximale sous contraintes:
- Puissance maison < 12kW
- Courant total < 40A
```

## 📈 Exemple de Fonctionnement

**Scénario**: Mode HP, production solaire variable

```
T=0s    Grid=+1200W  → Charge=0A     (import réseau)
T=300s  Grid=-2000W  → Charge=4.3A   (surplus détecté)  
T=305s  Grid=+200W   → Charge=3.1A   (PID réduit)
T=310s  Grid=-50W    → Charge=3.4A   (équilibre fin)
```

## 🔗 Topics MQTT

### Entrées (Écoute)
- `energy/grid/power` - Puissance réseau (W)
- `energy/tariff/state` - État HP/HC

### Format des Messages
```json
// Puissance réseau
{
  "power": -1500.0,
  "timestamp": "2024-01-01T12:00:00Z"  
}

// État tarifaire  
"HC"  // ou "HP"
```

## 🛡️ Sécurités

- **Anti-windup PID** si saturation
- **Arrêt sécurisé** si import > 50W persistant
- **Timeout données** MQTT (5min max)
- **Courant minimum** 6A par borne
- **Limites absolues** 0-40A total

## 🏗️ Architecture

```
cmd/main.go              # Point d'entrée
├── internal/config/     # Configuration YAML
├── internal/models/     # Structures données
├── internal/ocpp/       # Serveur WebSocket OCPP  
├── internal/mqtt/       # Client MQTT
└── internal/charging/   # Régulateur PID + logique
```

## 📋 Logs et Debugging

```bash
# Voir la régulation PID
grep "PID:" ocpp-server.log

# Voir les allocations de courant
grep "Allocated" ocpp-server.log  

# Voir les mesures MQTT
grep "Grid power updated" ocpp-server.log
```

## 🔧 Réglage du PID

**Trop oscillant?** → Réduire `pid_kp` et `pid_kd`
**Trop lent?** → Augmenter `pid_kp` 
**Erreur statique?** → Augmenter `pid_ki`

**Valeurs recommandées**:
- `pid_kp`: 0.001-0.005 (départ: 0.002)
- `pid_ki`: 0.0001-0.001 (départ: 0.0005)  
- `pid_kd`: 0.00001-0.0001 (départ: 0.00002)

## 🐳 Docker

### Build
```bash
docker build -t ocpp-server .
```

### Stack complète
```bash
docker-compose up -d
```

Services:
- `ocpp-server:8080` - Serveur OCPP
- `mqtt:1883` - Broker Mosquitto