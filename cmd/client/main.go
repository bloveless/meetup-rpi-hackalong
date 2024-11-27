package main

import (
	"context"
	"io"
	"log"
	"net"
	"os"
	"time"

	mqtt "github.com/soypat/natiu-mqtt"
	"golang.org/x/exp/rand"
)

func main() {
	// Create new client.
	client := mqtt.NewClient(mqtt.ClientConfig{
		Decoder: mqtt.DecoderNoAlloc{UserBuffer: make([]byte, 1500)},
		OnPub: func(_ mqtt.Header, _ mqtt.VariablesPublish, r io.Reader) error {
			message, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			log.Println("received message:", string(message))
			return nil
		},
	})
	const TOPICNAME = "/tinygo-pico-test"
	// Set the connection parameters and set the Client ID to "salamanca".
	var varConn mqtt.VariablesConnect
	varConn.SetDefaultMQTT([]byte("salamanca"))
	varConn.Username = []byte("brennon")
	password, err := os.ReadFile("virtus-GoMeet-brennon.jwt")
	if err != nil {
		panic(err)
	}
	varConn.Password = password
	rng := rand.New(rand.NewSource(1))

	log.Printf("Here: 1")

	// Define an inline function that connects the MQTT client automatically.
	// Is inline so it is contained within example.
	tryConnect := func() error {
		// Get a transport for MQTT packets using the local host and default MQTT port (1883).
		conn, err := net.Dial("tcp", "nats.netfung.com:1883")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		err = client.Connect(ctx, conn, &varConn) // Connect to server.
		if err != nil {
			return err
		}

		// On succesful connection subscribe to topic.
		ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		vsub := mqtt.VariablesSubscribe{
			TopicFilters: []mqtt.SubscribeRequest{
				{TopicFilter: []byte(TOPICNAME), QoS: mqtt.QoS0}, // Only support QoS0 for now.
			},
			PacketIdentifier: uint16(rng.Int31()),
		}

		return client.Subscribe(ctx, vsub)
	}

	// Attempt first connection and fail immediately if that does not work.
	err = tryConnect()
	if err != nil {
		log.Println(err)
		return
	}

	// Call read goroutine. Read goroutine will also handle reconnection
	// when client disconnects.
	go func() {
		for {
			if !client.IsConnected() {
				log.Printf("attempting to reconnect")
				time.Sleep(time.Second)
				tryConnect()
				continue
			}
			log.Printf("Here: 6")
			err = client.HandleNext()
			if err != nil {
				log.Println("HandleNext failed:", err)
			}
		}
	}()

	// Main program logic.
	for {
		log.Printf("waiting for a message")
		time.Sleep(10 * time.Second)
	}
}
