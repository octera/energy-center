package main

import (
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"log"
	"os"
	"strings"
	. "teleinfo2mqtt/home-assistant"
	"teleinfo2mqtt/teleinfo"
	"time"
)

const ProgNameMqtt string = "teleinfo2mqtt"
const WatchdogTimeout = 1 * time.Minute
const baseTopic = "teleinfo"

func watchdogFired() {
	log.Fatal("Watchdog fired, killing process")
	os.Exit(4)
}

func main() {
	var url string
	var serialDevice string
	var mode string

	flag.StringVar(&url, "url", "192.168.0.21:1883", "mqtt server")
	flag.StringVar(&serialDevice, "port", "/dev/serial/by-id/usb-1a86_USB2.0-Serial-if00-port0", "serial port")
	flag.StringVar(&mode, "mode", "standard", "Teleinfo mode standard or historic")

	flag.Parse()

	if mode != "historic" && mode != "standard" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	port, err := teleinfo.OpenPort(serialDevice, mode)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer port.Close()

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

	sendHomeAssistantConfig(client)

	watchdog := time.AfterFunc(WatchdogTimeout, watchdogFired)

	// Read Teleinfo frames and send them into mqtt
	go handleFrame(teleinfo.NewReader(port, &mode), client, watchdog)

	<-(chan int)(nil) //trick to wait for ever

	fmt.Printf("%s: Reached end of app, should not happens\n", ProgNameMqtt)
}

func sendHomeAssistantConfig(client mqtt.Client) {
	device := Device{Name: ProgNameMqtt, Identifiers: []string{ProgNameMqtt}}
	configItems := []ConfigurationItem{
		{DeviceClass: ApparentPower, UnitOfMeasurement: VA, Device: device, StateClass: "measurement",
			UniqueId: ProgNameMqtt + "_SINSTS", Name: "Puissance Apparente", StateTopic: baseTopic + "/" + "SINSTS"},
		{DeviceClass: ApparentPower, UnitOfMeasurement: VA, Device: device, StateClass: "measurement",
			UniqueId: ProgNameMqtt + "_SINSTI", Name: "Puissance Injectee", StateTopic: baseTopic + "/" + "SINSTI"},
		{DeviceClass: Current, UnitOfMeasurement: A, Device: device, StateClass: "measurement",
			UniqueId: ProgNameMqtt + "_IRMS1", Name: "Intensite", StateTopic: baseTopic + "/" + "IRMS1"},
		{DeviceClass: Voltage, UnitOfMeasurement: V, Device: device, StateClass: "measurement",
			UniqueId: ProgNameMqtt + "_URMS1", Name: "Tension", StateTopic: baseTopic + "/" + "URMS1"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EASF01", Name: "Index Bleu HC", StateTopic: baseTopic + "/" + "EASF01"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EASF02", Name: "Index Bleu HP", StateTopic: baseTopic + "/" + "EASF02"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EASF03", Name: "Index Blanc HC", StateTopic: baseTopic + "/" + "EASF03"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EASF04", Name: "Index Blanc HP", StateTopic: baseTopic + "/" + "EASF04"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EASF05", Name: "Index Rouge HC", StateTopic: baseTopic + "/" + "EASF05"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EASF06", Name: "Index Rouge HP", StateTopic: baseTopic + "/" + "EASF06"},
		{DeviceClass: Energy, UnitOfMeasurement: Wh, Device: device, StateClass: "total_increasing",
			UniqueId: ProgNameMqtt + "_EAIT", Name: "Index Injection", StateTopic: baseTopic + "/" + "EAIT"},
		{Device: device,
			UniqueId: ProgNameMqtt + "_LTARF", Name: "Tarif", StateTopic: baseTopic + "/" + "LTARF"},
	}
	SendConfigurationToHa(client, configItems, ProgNameMqtt)
}

func handleFrame(reader teleinfo.Reader, client mqtt.Client, watchdog *time.Timer) {
	fmt.Printf("handleFrame\n")
	for {
		frame, err := reader.ReadFrame()
		if err != nil {
			fmt.Printf("Error reading Teleinfo frame: %s\n", err)
			continue
		}
		for k, v := range frame.AsMap() {
			key := strings.Replace(k, "+", "p", -1)
			value := strings.TrimSpace(strings.Replace(v, "\t", " ", -1))
			token := client.Publish(baseTopic+"/"+key, 0, true, value)
			token.Wait()
			watchdog.Reset(WatchdogTimeout)
		}
	}
}
