#!/bin/bash

# Script de test rapide sans Docker

echo "🧪 Test Rapide OCPP - Sans Docker"
echo "================================="
echo ""

# Vérifier si mosquitto est installé localement
if command -v mosquitto &> /dev/null; then
    echo "✅ Mosquitto trouvé localement"
    USE_LOCAL_MQTT=true
else
    echo "📦 Mosquitto non trouvé, utilisation de Docker..."
    USE_LOCAL_MQTT=false
fi

# Configuration de test simple
cat > config.yaml << EOF
server:
  host: "0.0.0.0"
  port: 8080

mqtt:
  broker: "tcp://localhost:1883"
  username: ""
  password: ""
  topics:
    grid_power: "energy/grid/power"
    hphc_state: "energy/tariff/state"

charging:
  max_total_current: 40.0
  max_house_power: 12000.0
  smoothing_factor: 0.1
  update_interval: 3
  station1_priority: 1
  station2_priority: 2
  
  # PID plus réactif pour les tests
  pid_kp: 0.002
  pid_ki: 0.0005
  pid_kd: 0.00002
  grid_target_power: 0.0
EOF

echo "📝 Configuration de test créée (cycle de 3s pour tests rapides)"

# Démarrer MQTT
if [ "$USE_LOCAL_MQTT" = true ]; then
    echo "🚀 Démarrage de Mosquitto local..."
    mosquitto -d -p 1883
    MQTT_PID=$!
    sleep 2
else
    echo "🐳 Démarrage du broker MQTT Docker..."
    docker run -d --rm --name mqtt-test -p 1883:1883 eclipse-mosquitto:2.0
    sleep 3
fi

# Vérifier MQTT
echo "🔍 Test de connexion MQTT..."
if command -v mosquitto_pub &> /dev/null; then
    mosquitto_pub -h localhost -p 1883 -t test -m "connection_test"
    if [ $? -eq 0 ]; then
        echo "✅ MQTT opérationnel"
    else
        echo "❌ MQTT non accessible"
        cleanup_and_exit
    fi
else
    echo "⚠️  mosquitto_pub non disponible, on suppose que MQTT fonctionne"
fi

# Compiler
echo "🔨 Compilation..."
go build -o ocpp-server ./cmd
if [ $? -ne 0 ]; then
    echo "❌ Erreur de compilation"
    cleanup_and_exit
fi

go build -o simulator mqtt_simulator.go
if [ $? -ne 0 ]; then
    echo "❌ Erreur de compilation du simulateur"
    cleanup_and_exit
fi

echo "✅ Compilation réussie"

# Démarrer le serveur
echo "🚀 Démarrage du serveur OCPP..."
echo "📊 Logs en temps réel dans ocpp-server.log"
./ocpp-server > ocpp-server.log 2>&1 &
OCPP_PID=$!

sleep 2

# Vérifier que le serveur fonctionne
if ! kill -0 $OCPP_PID 2>/dev/null; then
    echo "❌ Le serveur OCPP n'a pas démarré"
    echo "📄 Dernières lignes des logs:"
    tail -10 ocpp-server.log
    cleanup_and_exit
fi

echo "✅ Serveur OCPP démarré (PID: $OCPP_PID)"
echo ""
echo "🎯 Lancement du test de régulation PID..."
echo "   (Surveillez ocpp-server.log dans un autre terminal)"
echo ""

# Lancer le simulateur
./simulator

echo ""
echo "🧹 Nettoyage..."

cleanup_and_exit() {
    # Arrêter le serveur OCPP
    if [ ! -z "$OCPP_PID" ]; then
        kill $OCPP_PID 2>/dev/null
    fi
    
    # Arrêter MQTT
    if [ "$USE_LOCAL_MQTT" = true ]; then
        if [ ! -z "$MQTT_PID" ]; then
            kill $MQTT_PID 2>/dev/null
        fi
    else
        docker stop mqtt-test 2>/dev/null
    fi
    
    # Nettoyer
    rm -f config.yaml
    
    echo "✅ Nettoyage terminé"
    
    if [ -f ocpp-server.log ]; then
        echo ""
        echo "📊 Analyse rapide des résultats:"
        echo "   - Nombre de régulations PID: $(grep -c "PID:" ocpp-server.log)"
        echo "   - Nombre d'allocations: $(grep -c "Allocated" ocpp-server.log)" 
        echo "   - Messages MQTT reçus: $(grep -c "Grid power updated" ocpp-server.log)"
        echo ""
        echo "📋 Pour voir les détails:"
        echo "   grep 'PID:' ocpp-server.log"
        echo "   grep 'Allocated' ocpp-server.log"
    fi
    
    exit ${1:-0}
}

# Attendre l'arrêt
cleanup_and_exit