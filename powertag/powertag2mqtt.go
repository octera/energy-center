package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"log"
	"os"
	"strings"
	"time"
)

const ProgNameMqtt string = "powertag2mqtt"

func main() {
	var url string

	flag.StringVar(&url, "url", "192.168.0.20:1883", "mqtt server")
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
	opts := mqtt.NewClientOptions().AddBroker(url).SetClientID(ProgNameMqtt)
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

					token := client.Publish("powertag/"+tags["id"], 0, false, jsonStr)
					token.Wait()
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
