package main

import (
	"flag"
	"log/slog"
	"networks_nsu/lab5/proxy"
	"strconv"
)

const (
	min_user_port = 1024
	max_user_port = 65535
	buffer_size   = 1 << 15
)

func main() {
	port := flag.Int("port", 1080, "proxy server listening port")
	flag.Parse()

	if *port < min_user_port || *port > max_user_port {
		slog.Error("port must be in interval [1024, 65535]")
		return
	}

	addr := ":" + strconv.Itoa(*port)

	proxy := proxy.NewSocksProxy()

}
