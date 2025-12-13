package home_assistant

import (
	json2 "encoding/json"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"strings"
)

func SendConfigurationToHa(client mqtt.Client, config []ConfigurationItem, globalName string) {
	for _, configItem := range config {
		b, err := json2.Marshal(configItem)
		if err != nil {
			fmt.Println(err)
			return
		}
		name := globalName + "_" + strings.Replace(strings.ToLower(configItem.Name), " ", "_", -1)
		token := client.Publish("homeassistant/sensor/"+name+"/config", 0, true, b)
		token.Wait()
	}
}
