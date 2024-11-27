package main

import (
	"04-rpi-hackalong/common"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"machine"
	"net/netip"
	"strings"
	"time"

	mqtt "github.com/soypat/natiu-mqtt"
	"github.com/soypat/seqs"
	"github.com/soypat/seqs/stacks"
	"golang.org/x/exp/rand"
)

var (
	clientID = []byte("tinygo-mqtt-brennon")
	//go:embed mqtt.jwt
	passwd string
)

const (
	tcpbufsize    = 2030 // MTU - ethhdr - iphdr - tcphdr
	serverAddrStr = "54.189.191.127:1883"
)

type received struct {
	Num int
}

func main() {
	logger := slog.New(slog.NewTextHandler(machine.Serial, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	dhcpc, stack, dev, err := common.SetupWithDHCP(common.SetupConfig{
		Hostname: string(clientID),
		Logger:   logger,
		TCPPorts: 1, // For HTTP over TCP.
		UDPPorts: 2, // For DNS.
	})
	start := time.Now()
	if err != nil {
		panic("setup DHCP:" + err.Error())
	}

	routerhw, err := common.ResolveHardwareAddr(stack, dhcpc.Router())
	if err != nil {
		panic("router hwaddr resolving:" + err.Error())
	}

	// For some reason this won't resolve nats.netfung.com so I've hard coded the ip address
	// resolver, err := common.NewResolver(stack, dhcpc)
	// if err != nil {
	// 	panic("resolver create:" + err.Error())
	// }
	// addrs, err := resolver.LookupNetIP("nats.netfung.com")
	// if err != nil {
	// 	panic("DNS lookup failed:" + err.Error())
	// }

	svAddr, err := netip.ParseAddrPort(serverAddrStr)
	if err != nil {
		panic("parsing server address:" + err.Error())
	}

	rng := rand.New(rand.NewSource(uint64(time.Now().Sub(start))))
	clientAddr := netip.AddrPortFrom(stack.Addr(), uint16(rng.Intn(65535-1024)+1024))

	// Create new client.
	client := mqtt.NewClient(mqtt.ClientConfig{
		Decoder: mqtt.DecoderNoAlloc{make([]byte, 1500)},
		OnPub: func(_ mqtt.Header, _ mqtt.VariablesPublish, r io.Reader) error {
			message, _ := io.ReadAll(r)
			if len(message) > 0 {
				var m received
				json.NewDecoder(strings.NewReader(string(message))).Decode(&m)
				logger.Info("received message", slog.String("msg", string(message)), slog.Int("num", m.Num))

				if m.Num >= 5 {
					logger.Info("turning led on")
					dev.GPIOSet(0, true)
				} else {
					logger.Info("turning led off")
					dev.GPIOSet(0, false)
				}
			}
			return nil
		},
	})
	const TOPICNAME = "tinygo-pico-test"
	var varConn mqtt.VariablesConnect
	varConn.Username = []byte("brennon")
	varConn.Password = []byte(passwd)
	varConn.SetDefaultMQTT(clientID)

	conn, err := stacks.NewTCPConn(stack, stacks.TCPConnConfig{
		TxBufSize: tcpbufsize,
		RxBufSize: tcpbufsize,
	})
	if err != nil {
		panic("conn create:" + err.Error())
	}

	closeConn := func(err error) {
		logger.Error("tcpconn:closing", slog.Any("err", err))
		conn.Close()
		for !conn.State().IsClosed() {
			logger.Info("tcpconn:waiting", slog.String("state", conn.State().String()))
			time.Sleep(1000 * time.Millisecond)
		}
	}

	// Define an inline function that connects the MQTT client automatically.
	tryConnect := func() error {
		random := rng.Uint32()
		logger.Info("socket:listen")
		err = conn.OpenDialTCP(clientAddr.Port(), routerhw, svAddr, seqs.Value(random))
		if err != nil {
			return fmt.Errorf("unable to dial socket: %w", err)
		}
		retries := 50
		for conn.State() != seqs.StateEstablished && retries > 0 {
			time.Sleep(100 * time.Millisecond)
			retries--
		}
		if retries == 0 {
			logger.Info("socket:no-establish")
			return errors.New("did not establish connection")
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
		closeConn(err)
		return
	}

	// Call read goroutine. Read goroutine will also handle reconnection
	// when client disconnects.
	go func() {
		for {
			if !client.IsConnected() {
				logger.Error("client disconnected. reconnecting")
				time.Sleep(time.Second)
				tryConnect()
				// TODO: if tryConnect returns an error should the program panic or just retry forever like it does now
				continue
			}
			err = client.HandleNext()
			if err != nil {
				logger.Error("HandleNext failed", slog.Any("err", err))
			}
		}
	}()

	// Call Write goroutine and create a channel to serialize messages
	// that we want to send out.
	pubFlags, _ := mqtt.NewPublishFlags(mqtt.QoS0, false, false)
	varPub := mqtt.VariablesPublish{
		TopicName: []byte(TOPICNAME),
	}
	txQueue := make(chan []byte, 10)
	go func() {
		for {
			if !client.IsConnected() {
				time.Sleep(time.Second)
				continue
			}
			message := <-txQueue
			varPub.PacketIdentifier = uint16(rng.Int())
			// Loop until message is sent successfully. This guarantees
			// all messages are sent, even in events of disconnect.
			for {
				err := client.PublishPayload(pubFlags, varPub, message)
				if err == nil {
					break
				}
				time.Sleep(time.Second)
			}
		}
	}()

	// Main program logic.
	for {
		n := rng.Intn(10)
		message := fmt.Sprintf(`{"num": %d}`, n)
		txQueue <- []byte(message)
		time.Sleep(time.Second)
	}
}
