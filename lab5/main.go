package main

import (
	"context"
	"flag"
	"log/slog"
	"networks_nsu/lab5/proxy"
	"os"
	"os/signal"
	"strconv"
	"syscall"
)

const (
	min_user_port = 1024
	max_user_port = 65535
)

func main() {
	port := flag.Int("port", 1080, "proxy server listening port")
	flag.Parse()

	if *port < min_user_port || *port > max_user_port {
		slog.Error("port must be in interval [1024, 65535]")
		return
	}

	addr := ":" + strconv.Itoa(*port)

	serv := proxy.NewSocksProxy()
	if err := serv.Up(addr); err != nil {
		return
	}

	servErr := make(chan error, 1)
	go func() {
		if err := serv.Serve(); err != nil {
			servErr <- err
		}
	}()

	ctx := context.Background()
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	slog.Info("shutdown signal received")

	if err := serv.Close(); err != nil {
		slog.Error("server graceful shutdown failed", "error", err)
	} else {
		slog.Info("server gracefully stopped")
	}
}
