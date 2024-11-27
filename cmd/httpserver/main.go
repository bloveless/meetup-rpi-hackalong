package main

import (
	"04-rpi-hackalong/common"
	"context"
	_ "embed"
	"errors"
	"io"
	"log/slog"
	"machine"
	"net"
	"os"
	"time"

	"github.com/soypat/cyw43439"
	"github.com/soypat/peasocket"
	"github.com/soypat/seqs/httpx"
)

const (
	maxconns   = 3
	tcpbufsize = 2030 // MTU - ethhdr - iphdr - tcphdr
)

var (
	// We embed the html file in the binary so that we can edit
	// index.html with pretty syntax highlighting.
	//
	//go:embed index.html
	webPage      []byte
	dev          *cyw43439.Device
	lastLedState bool
)

// This is our HTTP hander. It handles ALL incoming requests. Path routing is left
// as an excercise to the reader.
func HTTPHandler(respWriter io.Writer, resp *httpx.ResponseHeader, req *httpx.RequestHeader) {
	uri := string(req.RequestURI())
	resp.SetConnectionClose()
	switch uri {
	case "/":
		println("Got webpage request!")
		resp.SetContentType("text/html")
		resp.SetContentLength(len(webPage))
		respWriter.Write(resp.Header())
		respWriter.Write(webPage)

	case "/toggle-led":
		println("Got toggle led request!")
		respWriter.Write(resp.Header())
		lastLedState = !lastLedState
		dev.GPIOSet(0, lastLedState)

	default:
		println("Path not found:", uri)
		resp.SetStatusCode(404)
		respWriter.Write(resp.Header())
	}
}

func main() {
	ctx := context.Background()

	logger := slog.New(slog.NewTextHandler(machine.Serial, &slog.HandlerOptions{
		Level: slog.LevelDebug - 2,
	}))
	time.Sleep(time.Second)

	_, _, devlocal, err := common.SetupWithDHCP(common.SetupConfig{
		Hostname: "TCP-pico",
		Logger:   logger,
		TCPPorts: 1,
	})
	dev = devlocal
	if err != nil {
		logger.Error("setup DHCP:" + err.Error())
		os.Exit(1)
	}
	// // Start TCP server.
	// const listenPort = 80
	// listener, err := stacks.NewTCPListener(stack, stacks.TCPListenerConfig{
	// 	MaxConnections: maxconns,
	// 	ConnTxBufSize:  tcpbufsize,
	// 	ConnRxBufSize:  tcpbufsize,
	// })
	// if err != nil {
	// 	logger.Error("listener create:" + err.Error())
	// 	os.Exit(1)
	// }
	// err = listener.StartListening(listenPort)
	// if err != nil {
	// 	logger.Error("listener start:" + err.Error())
	// 	os.Exit(1)
	// }

	logger.Info("IS LINK UP: %b", dev.IsLinkUp())

	client := peasocket.NewClient("ws://192.168.0.180:8433", nil, nil)
	err = client.Dial(ctx, nil)
	if err != nil {
		logger.Error("while dialing:", err)
		os.Exit(1)
	}
	defer client.CloseWebsocket(&peasocket.CloseError{
		Status: peasocket.StatusGoingAway,
		Reason: []byte("peacho finalized"),
	})
	go func() {
		for {
			err := client.HandleNextFrame()
			if client.Err() != nil {
				logger.Error("failure, retrying:", client.Err())
				client.CloseConn(client.Err())
				client.Dial(ctx, nil)
				time.Sleep(time.Second)
				continue
			}
			if err != nil {
				logger.Error("read next frame failed:", err)
				time.Sleep(500 * time.Millisecond)
			}
		}
	}()
	// Exponential Backoff algorithm to not saturate
	// the process with calls to NextMessageReader
	// https://en.wikipedia.org/wiki/Exponential_backoff
	backoff := peasocket.ExponentialBackoff{MaxWait: 500 * time.Millisecond}
	for {
		msg, _, err := client.BufferedMessageReader()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || client.Err() != nil {
				logger.Error("websocket closed:", client.Err())
			}
			backoff.Miss()
			continue
		}
		backoff.Hit()
		b, err := io.ReadAll(msg)
		if err != nil {
			logger.Error("while reading message:", err)
		}
		logger.Info("echoing message:", string(b))
		err = client.WriteMessage(b)
		if err != nil {
			logger.Error("while echoing message:", err)
		}
	}

	// // Reuse the same buffers for each connection to avoid heap allocations.
	// var req httpx.RequestHeader
	// var resp httpx.ResponseHeader
	// buf := bufio.NewReaderSize(nil, 1024)
	// logger.Info("listening",
	// 	slog.String("addr", "http://"+listenAddr.String()),
	// )
	//
	// for {
	// 	conn, err := listener.Accept()
	// 	if err != nil {
	// 		logger.Error("listener accept:", slog.String("err", err.Error()))
	// 		time.Sleep(time.Second)
	// 		continue
	// 	}
	// 	logger.Info("new connection", slog.String("remote", conn.RemoteAddr().String()))
	// 	err = conn.SetDeadline(time.Now().Add(connTimeout))
	// 	if err != nil {
	// 		conn.Close()
	// 		logger.Error("conn set deadline:", slog.String("err", err.Error()))
	// 		continue
	// 	}
	// 	buf.Reset(conn)
	// 	err = req.Read(buf)
	// 	if err != nil {
	// 		logger.Error("hdr read:", slog.String("err", err.Error()))
	// 		conn.Close()
	// 		continue
	// 	}
	// 	resp.Reset()
	// 	HTTPHandler(conn, &resp, &req)
	// 	// time.Sleep(100 * time.Millisecond)
	// 	conn.Close()
	// }
}
