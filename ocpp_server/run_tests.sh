#!/bin/bash

# Script pour lancer les tests du serveur OCPP avec simulation MQTT

echo "🚀 Lancement des tests du serveur OCPP"
echo "======================================"

# Vérifier que docker-compose est installé
if ! command -v docker-compose &> /dev/null; then
    echo "❌ docker-compose n'est pas installé"
    exit 1
fi

# Créer le fichier de configuration pour les tests
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
  update_interval: 5
  station1_priority: 1
  station2_priority: 2
  
  # PID Controller pour les tests
  pid_kp: 0.002
  pid_ki: 0.0005
  pid_kd: 0.00002
  grid_target_power: 0.0
EOF

echo "📝 Configuration de test créée"

# Démarrer MQTT broker
echo "🐳 Démarrage du broker MQTT..."
docker-compose up -d mqtt

# Attendre que MQTT soit prêt
echo "⏳ Attente du démarrage MQTT..."
sleep 5

# Vérifier la connexion MQTT
echo "🔍 Vérification de la connexion MQTT..."
if ! docker exec ocpp_server_mqtt_1 mosquitto_pub -h localhost -t test -m "test" 2>/dev/null; then
    echo "❌ MQTT broker non accessible"
    docker-compose logs mqtt
    exit 1
fi

echo "✅ MQTT broker opérationnel"

# Compiler le serveur OCPP
echo "🔨 Compilation du serveur OCPP..."
go build -o ocpp-server ./cmd
if [ $? -ne 0 ]; then
    echo "❌ Erreur de compilation"
    exit 1
fi

# Compiler le simulateur MQTT
echo "🔨 Compilation du simulateur MQTT..."
go build -o mqtt-simulator test_mqtt_simulator.go
if [ $? -ne 0 ]; then
    echo "❌ Erreur de compilation du simulateur"
    exit 1
fi

echo "✅ Compilation réussie"

# Démarrer le serveur OCPP en arrière-plan
echo "🚀 Démarrage du serveur OCPP..."
./ocpp-server > ocpp-server.log 2>&1 &
OCPP_PID=$!

# Attendre que le serveur démarre
sleep 3

# Vérifier que le serveur fonctionne
if ! kill -0 $OCPP_PID 2>/dev/null; then
    echo "❌ Le serveur OCPP n'a pas démarré correctement"
    cat ocpp-server.log
    docker-compose down
    exit 1
fi

echo "✅ Serveur OCPP démarré (PID: $OCPP_PID)"
echo ""
echo "📊 Logs du serveur en temps réel:"
echo "   tail -f ocpp-server.log"
echo ""
echo "🧪 Lancement des tests MQTT..."
echo ""

# Lancer le simulateur de tests
./mqtt-simulator

# Nettoyer
echo ""
echo "🧹 Nettoyage..."
kill $OCPP_PID 2>/dev/null
docker-compose down
rm -f config.yaml

echo "✅ Tests terminés. Logs sauvegardés dans ocpp-server.log"
echo ""
echo "📋 Pour analyser les résultats:"
echo "   grep 'PID:' ocpp-server.log"
echo "   grep 'Allocated' ocpp-server.log"
echo "   grep 'Grid power' ocpp-server.log"