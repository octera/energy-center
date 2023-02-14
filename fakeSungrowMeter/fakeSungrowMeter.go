package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/goburrow/serial"
	mbserver "github.com/tbrandon/mbserver"
)

const ProgNameMqtt string = "fakeSungrowPower"
const WatchdogTimeout = 3 * time.Minute

var watchdogMqtt = time.AfterFunc(WatchdogTimeout, watchdogMqttFired)
var watchdogModbus = time.AfterFunc(WatchdogTimeout, watchdogModbusFired)

var gridPower int32 = 0
var gridIndex int32 = 0
var injecIndex int32 = 0

func watchdogMqttFired() {
	log.Fatal("Watchdog mqtt fired, killing process")
	os.Exit(4)
}
func watchdogModbusFired() {
	log.Fatal("Watchdog modbus fired, killing process")
	os.Exit(4)
}

func listenMqttGrid(client mqtt.Client) {
	client.Subscribe("powerinfo/grid", 0, func(client mqtt.Client, msg mqtt.Message) {
		var p, _ = strconv.Atoi(string(msg.Payload()))
		gridPower = int32(p)
		watchdogMqtt.Reset(WatchdogTimeout)
	})
}
func listenMqttGridIndex(client mqtt.Client) {
	client.Subscribe("powerinfo/totalIndex", 0, func(client mqtt.Client, msg mqtt.Message) {
		var p, _ = strconv.Atoi(string(msg.Payload()))
		gridIndex = int32(p)
		watchdogMqtt.Reset(WatchdogTimeout)
	})
}
func listenMqttInjectIndex(client mqtt.Client) {
	client.Subscribe("powerinfo/totalInjIndex", 0, func(client mqtt.Client, msg mqtt.Message) {
		var p, _ = strconv.Atoi(string(msg.Payload()))
		injecIndex = int32(p)
		watchdogMqtt.Reset(WatchdogTimeout)
	})
}

func main() {
	var url string
	var serialDevice string

	flag.StringVar(&url, "url", "192.168.0.20:1883", "mqtt server")
	flag.StringVar(&serialDevice, "port", "/dev/serial/by-id/usb-1a86_USB2.0-Ser_-if00-port0", "serial port")

	flag.Parse()

	mqttClient := CreateMqttClient(url)
	modbusServer := CreateModbusServer(serialDevice)
	defer modbusServer.Close()

	go listenMqttGrid(mqttClient)
	go listenMqttGridIndex(mqttClient)
	go listenMqttInjectIndex(mqttClient)

	modbusServer.RegisterFunctionHandler(3, modbusMessageHandler)

	<-(chan int)(nil) //trick to wait forever

	fmt.Printf("%s: Reached end of app, should not happens\n", ProgNameMqtt)
}

func modbusMessageHandler(server *mbserver.Server, frame mbserver.Framer) ([]byte, *mbserver.Exception) {
	frameDate := frame.GetData()
	register := int(binary.BigEndian.Uint16(frameDate[0:2]))
	numRegs := int(binary.BigEndian.Uint16(frameDate[2:4]))

	dataSize := numRegs * 2

	data := make([]byte, 1+dataSize)
	for i := 0; i < 1+dataSize; i++ {
		data[i] = 0
	}
	data[0] = byte(dataSize)

	switch register {
	case 63:
		fmt.Printf("Requesting %d with %d register count\n", register, numRegs)
	case 10: // 12 registers
		var byteBuffer = make([]byte, 4)
		var indexDiv = gridIndex / 10
		binary.BigEndian.PutUint32(byteBuffer, uint32(indexDiv)) // Current forward active total electric energy : scale is 1 increment per 10wh
		data[1] = byteBuffer[0]
		data[2] = byteBuffer[1]
		data[3] = byteBuffer[2]
		data[4] = byteBuffer[3]
		binary.BigEndian.PutUint32(byteBuffer, uint32(0)) //Current forward active spike electric energy ?
		data[5] = byteBuffer[0]
		data[6] = byteBuffer[1]
		data[7] = byteBuffer[2]
		data[8] = byteBuffer[3]
		binary.BigEndian.PutUint32(byteBuffer, uint32(0)) // Current forward active peak electric energy
		data[9] = byteBuffer[0]
		data[10] = byteBuffer[1]
		data[11] = byteBuffer[2]
		data[12] = byteBuffer[3]
		binary.BigEndian.PutUint32(byteBuffer, uint32(0)) // Current forward active flat electric energy
		data[13] = byteBuffer[0]
		data[14] = byteBuffer[1]
		data[15] = byteBuffer[2]
		data[16] = byteBuffer[3]
		binary.BigEndian.PutUint32(byteBuffer, uint32(0)) // Current forward active valley electric energy
		data[17] = byteBuffer[0]
		data[18] = byteBuffer[1]
		data[19] = byteBuffer[2]
		data[20] = byteBuffer[3]
		binary.BigEndian.PutUint32(byteBuffer, uint32(injecIndex/10)) // total export energy ? scale is 1 increment per 10wh
		data[21] = byteBuffer[0]
		data[22] = byteBuffer[1]
		data[23] = byteBuffer[2]
		data[24] = byteBuffer[3]
	case 97: // 3 register
		var byteBuffer = make([]byte, 2)
		binary.BigEndian.PutUint16(byteBuffer, uint16(220))
		data[1] = byteBuffer[0] //Voltage A
		data[2] = byteBuffer[1]
		data[3] = 0 //Voltage B
		data[4] = 0
		data[5] = 0 //Voltage C
		data[6] = 0
	case 119: // 5 seconds -> 1 register
		// frequency
	case 356: // 8 register
		watchdogModbus.Reset(WatchdogTimeout)
		var powerB = make([]byte, 4)
		binary.BigEndian.PutUint32(powerB, uint32(gridPower))
		// Copy in first position
		data[1] = powerB[0]
		data[2] = powerB[1]
		data[3] = powerB[2]
		data[4] = powerB[3]
		// Copy in 4th position
		data[13] = powerB[0]
		data[14] = powerB[1]
		data[15] = powerB[2]
		data[16] = powerB[3]
	default:
		fmt.Printf("Requesting %d with %d register count\n", register, numRegs)
	}

	return data, &mbserver.Success
}

func CreateModbusServer(device string) *mbserver.Server {
	serv := mbserver.NewServer()
	serv.Debug = true
	err := serv.ListenRTU(&serial.Config{
		Address:  device,
		BaudRate: 9600,
		DataBits: 8,
		StopBits: 1,
		Parity:   "N",
		Timeout:  10 * time.Second})
	if err != nil {
		fmt.Errorf("failed to listen, got %v\n", err)
	}

	return serv
}

func CreateMqttClient(url string) mqtt.Client {
	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().AddBroker(url).SetClientID(ProgNameMqtt)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(1 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	fmt.Printf("%s: connected to %s\n", ProgNameMqtt, url)
	return client
}
