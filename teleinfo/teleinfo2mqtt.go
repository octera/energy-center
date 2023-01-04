package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/tarm/serial"
	"log"
	"os"
	"strings"
	"time"
)

const ProgNameMqtt string = "teleinfo2mqtt"
const WatchdogTimeout = 1 * time.Minute

func watchdogFired() {
	log.Fatal("Watchdog fired, killing process")
	os.Exit(4)
}
func main2() {
	toto := "EASF01     021456863       E"
	res, err := parseLine(toto)
	if err != nil {
		fmt.Printf("Err: %s\n", err)

	} else {
		fmt.Printf("%s: %s\n", res[0], res[1])
	}
}

func main() {
	var url string
	var port string
	var baud int

	flag.StringVar(&url, "url", "192.168.0.20:1883", "mqtt server")
	flag.StringVar(&port, "port", "/dev/ttyUSB0", "serial port")
	flag.IntVar(&baud, "baud", 9600, "baud")

	flag.Parse()

	c := &serial.Config{Name: port, Baud: baud, Size: 7, Parity: serial.ParityEven, StopBits: serial.Stop1}
	s, err := serial.OpenPort(c)
	if err != nil {
		log.Fatal(err)
		os.Exit(2)
	}

	mqtt.ERROR = log.New(os.Stdout, "", 0)
	opts := mqtt.NewClientOptions().AddBroker(url).SetClientID(ProgNameMqtt)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetPingTimeout(1 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		panic(token.Error())
	}

	fmt.Printf("%s: connected to %s\n", ProgNameMqtt, url)

	watchdog := time.AfterFunc(WatchdogTimeout, watchdogFired)

	lnscan := bufio.NewScanner(s)
	for lnscan.Scan() {
		line := lnscan.Text()
		parsed, err := parseLine(line)
		if err != nil {
			fmt.Printf("%s: Bad paquet received:  -->%s<--\n", ProgNameMqtt, line)
		}
		if parsed != nil {
			//fmt.Printf("%s: Going to publish on '%s' ->%s<-\n", ProgNameMqtt, parsed[0], parsed[1])
			token := client.Publish("teleinfo/"+parsed[0], 0, false, parsed[1])
			token.Wait()
		}
		watchdog.Reset(WatchdogTimeout)
	}
	fmt.Printf("%s: Reached end of app, should not happens\n", ProgNameMqtt)
}

func parseLine(line string) ([]string, error) {
	if len(line) < 3 {
		return nil, nil
	}
	//TODO manage checksum -> checksum := line[len(line)-1] // get last char which is checksum
	line = line[:len(line)-1] //Remove last char
	splitted := strings.Fields(line)
	if len(splitted) < 2 {
		return nil, errors.New("bad parse")
	}
	if len(splitted[0]) == 0 && len(splitted[1]) == 0 {
		return nil, errors.New("bad length")
	}
	key := strings.Replace(splitted[0], "+", "p", -1)
	value := strings.Join(splitted[1:], " ")
	return []string{key, value}, nil

}
