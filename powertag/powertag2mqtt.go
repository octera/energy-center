package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"golang.org/x/exp/slices"
	"log"
	"os"
	. "powertag2mqtt/home-assistant"
	"strings"
	"time"
)

const ProgNameMqtt string = "powertag2mqtt"

var powertagConfigSent []string = []string{}

func sendHomeAssistantConfig(client mqtt.Client, powertagId string) {
	if slices.Contains(powertagConfigSent, powertagId) {
		return
	}
	powertagConfigSent = append(powertagConfigSent, powertagId)

	appName := ProgNameMqtt + "_" + powertagId
	baseTopic := "powertag/" + powertagId
	device := Device{Name: appName, Identifiers: []string{appName}}
	configItems := []ConfigurationItem{
		{DeviceClass: Current, UnitOfMeasurement: A, Device: device, StateClass: "measurement",
			StateTopic: baseTopic + "/current_p1",
			UniqueId:   appName + "_current_p1", Name: "Intensite"},
		{DeviceClass: Power, UnitOfMeasurement: W, Device: device, StateClass: "measurement",
			StateTopic: baseTopic + "/power_p1_active",
			UniqueId:   appName + "power_p1_active", Name: "Puissance Active"},
		{DeviceClass: Power, UnitOfMeasurement: W, Device: device, StateClass: "measurement",
			StateTopic: baseTopic + "/total_power_active",
			UniqueId:   appName + "_total_power_active", Name: "Puissance Active Totale"},
		{DeviceClass: ApparentPower, UnitOfMeasurement: VA, Device: device, StateClass: "measurement",
			StateTopic: baseTopic + "/total_power_apparent",
			UniqueId:   appName + "_total_power_apparent", Name: "Puissance Apparente Totale"},
		{DeviceClass: Voltage, UnitOfMeasurement: V, Device: device, StateClass: "measurement",
			StateTopic: baseTopic + "/voltage_p1",
			UniqueId:   appName + "_voltage_p1", Name: "Tension"},
		{DeviceClass: PowerFactor, Device: device, StateClass: "measurement",
			StateTopic: baseTopic + "/power_factor",
			UniqueId:   appName + "_power_factor", Name: "Power Factor"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/partial_energy_p1_tx",
			UniqueId:   appName + "_partial_energy_p1_tx", Name: "Partial Energy P1 TX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/partial_energy_tx",
			UniqueId:   appName + "_partial_energy_tx", Name: "Partial Energy TX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/total_energy_p1_tx",
			UniqueId:   appName + "_total_energy_p1_tx", Name: "Total Energy P1 TX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/total_energy_tx",
			UniqueId:   appName + "_total_energy_tx", Name: "Total Energy TX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/partial_energy_p1_rx",
			UniqueId:   appName + "_partial_energy_p1_rx", Name: "Partial Energy P1 RX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/partial_energy_rx",
			UniqueId:   appName + "_partial_energy_rx", Name: "Partial Energy RX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/total_energy_p1_rx",
			UniqueId:   appName + "_total_energy_p1_rx", Name: "Total Energy P1 RX"},
		{DeviceClass: Energy, UnitOfMeasurement: KWh, Device: device, StateClass: "total_increasing",
			StateTopic: baseTopic + "/total_energy_rx",
			UniqueId:   appName + "_total_energy_rx", Name: "Total Energy RX"},
	}
	SendConfigurationToHa(client, configItems, appName)
}

func main() {
	var url string

	flag.StringVar(&url, "url", "192.168.0.21:1883", "mqtt server")
	flag.Parse()

	stat, _ := os.Stdin.Stat()
	if stat.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintf(os.Stderr, "%s: no data on stdin\n", ProgNameMqtt)
		fmt.Fprintf(os.Stderr, "%s expects data to be piped to stdin, i.e.:\n", ProgNameMqtt)
		fmt.Fprintf(os.Stderr, "    powertagd | powertag2influx\n")
		os.Exit(2)
	}

	mqtt.DEBUG = log.New(os.Stdout, "", 0)
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().
		AddBroker(url).
		SetClientID(ProgNameMqtt).
		SetUsername("opas").
		SetPassword("opas")
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(1 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	fmt.Printf("%s: connected to %s\n", ProgNameMqtt, url)

	lnscan := bufio.NewScanner(os.Stdin)
	for lnscan.Scan() {
		line := lnscan.Text()
		fmt.Printf("%s: data: %s\n", ProgNameMqtt, line)
		if strings.HasPrefix(line, "powertag,") {
			sanitized := strings.Replace(line, "powertag,", "", -1)
			splitted := strings.Split(sanitized, " ")
			if len(splitted) == 3 {
				tags := asMap(splitted[0])
				measures := asMap(splitted[1])
				// ts := splitted[2]

				_, idExist := tags["id"]
				if idExist {
					jsonStr, _ := json.Marshal(measures)
					sendHomeAssistantConfig(client, tags["id"])
					token := client.Publish("powertag/"+tags["id"], 0, false, jsonStr)
					token.Wait()
					for key, element := range measures {
						token := client.Publish("powertag/"+tags["id"]+"/"+key, 0, false, element)
						token.Wait()
					}
				}

			}
		}
	}
}

func asMap(inputString string) map[string]string {
	m := make(map[string]string)

	entry := strings.Split(inputString, ",")
	for _, s := range entry {
		split := strings.Split(s, "=")
		m[split[0]] = split[1]
	}

	return m
}
