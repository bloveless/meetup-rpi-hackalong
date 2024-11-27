package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"time"

	mqtt "github.com/soypat/natiu-mqtt"
)

var (
	pubFlags, _ = mqtt.NewPublishFlags(mqtt.QoS0, false, false)
	pubVar      = mqtt.VariablesPublish{
		TopicName:        []byte("tinygo-pico-test/#"),
		PacketIdentifier: 0xc0fe,
	}
)

func main() {
	// Create new client.
	client := mqtt.NewClient(mqtt.ClientConfig{
		Decoder: mqtt.DecoderNoAlloc{UserBuffer: make([]byte, 1500)},
		OnPub: func(_ mqtt.Header, _ mqtt.VariablesPublish, r io.Reader) error {
			message, _ := io.ReadAll(r)
			log.Println("received message:", string(message))
			return nil
		},
	})

	// Get a transport for MQTT packets.
	const defaultMQTTPort = ":1883"
	conn, err := net.Dial("tcp", "nats.netfung.com"+defaultMQTTPort)
	if err != nil {
		fmt.Println(err)
		return
	}

	// Prepare for CONNECT interaction with server.
	var varConn mqtt.VariablesConnect
	varConn.SetDefaultMQTT([]byte("salamanca"))
	varConn.Username = []byte("brennon")
	b, err := os.ReadFile("virtus-GoMeet-brennon.jwt")
	if err != nil {
		panic(err)
	}
	varConn.Password = b
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	err = client.Connect(ctx, conn, &varConn) // Connect to server.
	cancel()
	if err != nil {
		// Error or loop until connect success.
		log.Fatalf("connect attempt failed: %v\n", err)
	}

	// Ping forever until error.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		pingErr := client.Ping(ctx)
		cancel()
		if pingErr != nil {
			log.Fatal("ping error: ", pingErr, " with disconnect reason:", client.Err())
		}
		log.Println("ping success!")

		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		pubErr := client.PublishPayload(pubFlags, pubVar, []byte("hello world"))
		cancel()
		if pubErr != nil {
			slog.Error("publish error", slog.Any("err", pubErr))
		}
		log.Println("publish success")

		time.Sleep(2 * time.Second)
	}
}
