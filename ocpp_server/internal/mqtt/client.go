package mqtt

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"ocpp-server/internal/config"
	"ocpp-server/internal/models"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

type Client struct {
	client mqtt.Client
	config *config.Config
	logger *logrus.Logger

	gridData  *models.GridData
	hphcState *models.HPHCState

	onGridPowerUpdate func(power float64)
	onHPHCUpdate      func(isOffPeak bool)
	onMQTTUpdate      func() // Callback pour notifier qu'une donnée MQTT a été mise à jour
}

type GridPowerMessage struct {
	Power     float64   `json:"power"`
	Timestamp time.Time `json:"timestamp"`
}

type HPHCMessage struct {
	State     string    `json:"state"`
	Timestamp time.Time `json:"timestamp"`
}

func NewClient(cfg *config.Config, logger *logrus.Logger) (*Client, error) {
	c := &Client{
		config:    cfg,
		logger:    logger,
		gridData:  models.NewGridData(),
		hphcState: models.NewHPHCState(),
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.MQTT.Broker)
	opts.SetClientID("ocpp-server")
	opts.SetUsername(cfg.MQTT.Username)
	opts.SetPassword(cfg.MQTT.Password)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetKeepAlive(60 * time.Second)

	opts.SetConnectionLostHandler(c.onConnectionLost)
	opts.SetOnConnectHandler(c.onConnect)

	c.client = mqtt.NewClient(opts)

	return c, nil
}

func (c *Client) Connect() error {
	c.logger.Info("Connecting to MQTT broker...")

	if token := c.client.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	c.logger.Info("Connected to MQTT broker")
	return nil
}

func (c *Client) Disconnect() {
	c.logger.Info("Disconnecting from MQTT broker...")
	c.client.Disconnect(250)
}

func (c *Client) SetCallbacks(onGridPower func(float64), onHPHC func(bool), onMQTTUpdate func()) {
	c.onGridPowerUpdate = onGridPower
	c.onHPHCUpdate = onHPHC
	c.onMQTTUpdate = onMQTTUpdate
}

func (c *Client) GetGridData() *models.GridData {
	return c.gridData
}

func (c *Client) GetHPHCState() *models.HPHCState {
	return c.hphcState
}

func (c *Client) onConnect(client mqtt.Client) {
	c.logger.Info("MQTT connected, subscribing to topics...")

	if c.config.MQTT.Topics.GridPower != "" {
		if token := client.Subscribe(c.config.MQTT.Topics.GridPower, 1, c.handleGridPowerMessage); token.Wait() && token.Error() != nil {
			c.logger.Errorf("Failed to subscribe to grid power topic: %v", token.Error())
		} else {
			c.logger.Infof("Subscribed to grid power topic: %s", c.config.MQTT.Topics.GridPower)
		}
	}

	if c.config.MQTT.Topics.HPHCState != "" {
		if token := client.Subscribe(c.config.MQTT.Topics.HPHCState, 1, c.handleHPHCMessage); token.Wait() && token.Error() != nil {
			c.logger.Errorf("Failed to subscribe to HP/HC topic: %v", token.Error())
		} else {
			c.logger.Infof("Subscribed to HP/HC topic: %s", c.config.MQTT.Topics.HPHCState)
		}
	}
}

func (c *Client) onConnectionLost(client mqtt.Client, err error) {
	c.logger.Errorf("MQTT connection lost: %v", err)
}

func (c *Client) handleGridPowerMessage(client mqtt.Client, msg mqtt.Message) {
	c.logger.Debugf("Received grid power message: %s", string(msg.Payload()))

	var power float64
	var err error

	if json.Valid(msg.Payload()) {
		var gridMsg GridPowerMessage
		if err = json.Unmarshal(msg.Payload(), &gridMsg); err == nil {
			power = gridMsg.Power
		}
	} else {
		power, err = strconv.ParseFloat(string(msg.Payload()), 64)
	}

	if err != nil {
		c.logger.Errorf("Failed to parse grid power value: %v", err)
		return
	}

	c.gridData.Update(power)
	c.logger.Infof("Grid power updated: %.2fW", power)

	if c.onGridPowerUpdate != nil {
		c.onGridPowerUpdate(power)
	}

	// Notifier que des données MQTT ont été mises à jour
	if c.onMQTTUpdate != nil {
		c.onMQTTUpdate()
	}
}

func (c *Client) handleHPHCMessage(client mqtt.Client, msg mqtt.Message) {
	c.logger.Debugf("Received HP/HC message: %s", string(msg.Payload()))

	var isOffPeak bool
	var err error

	if json.Valid(msg.Payload()) {
		var hphcMsg HPHCMessage
		if err = json.Unmarshal(msg.Payload(), &hphcMsg); err == nil {
			isOffPeak = (hphcMsg.State == "HC" || hphcMsg.State == "off-peak")
		}
	} else {
		payload := string(msg.Payload())
		isOffPeak = (payload == "HC" || payload == "off-peak" || payload == "1" || payload == "true")
	}

	if err != nil {
		c.logger.Errorf("Failed to parse HP/HC value: %v", err)
		return
	}

	c.hphcState.Update(isOffPeak)

	stateStr := "HP"
	if isOffPeak {
		stateStr = "HC"
	}
	c.logger.Infof("HP/HC state updated: %s", stateStr)

	if c.onHPHCUpdate != nil {
		c.onHPHCUpdate(isOffPeak)
	}
}
