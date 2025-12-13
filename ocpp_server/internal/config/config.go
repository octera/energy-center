package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	MQTT     MQTTConfig     `mapstructure:"mqtt"`
	Charging ChargingConfig `mapstructure:"charging"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

type MQTTConfig struct {
	Broker   string `mapstructure:"broker"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	Topics   Topics `mapstructure:"topics"`
}

type Topics struct {
	GridPower string `mapstructure:"grid_power"`
	HPHCState string `mapstructure:"hphc_state"`
}

type ChargingConfig struct {
	MaxTotalCurrent  float64 `mapstructure:"max_total_current"`
	MaxHousePower    float64 `mapstructure:"max_house_power"`
	SmoothingFactor  float64 `mapstructure:"smoothing_factor"`
	UpdateInterval   int     `mapstructure:"update_interval"`
	Station1Priority int     `mapstructure:"station1_priority"`
	Station2Priority int     `mapstructure:"station2_priority"`
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("charging.max_total_current", 40.0)
	viper.SetDefault("charging.max_house_power", 12000.0)
	viper.SetDefault("charging.smoothing_factor", 0.1)
	viper.SetDefault("charging.update_interval", 5)
	viper.SetDefault("charging.station1_priority", 1)
	viper.SetDefault("charging.station2_priority", 2)

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			fmt.Println("Config file not found, using defaults")
		} else {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	if config.MQTT.Broker == "" {
		config.MQTT.Broker = os.Getenv("MQTT_BROKER")
	}
	if config.MQTT.Username == "" {
		config.MQTT.Username = os.Getenv("MQTT_USERNAME")
	}
	if config.MQTT.Password == "" {
		config.MQTT.Password = os.Getenv("MQTT_PASSWORD")
	}

	return &config, nil
}
