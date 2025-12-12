package main

import (
	"encoding/binary"
	"flag"
	"io"
	"log/slog"
	"net"
	"strconv"
)

func main() {
	//parsing command line
	port := flag.Int("port", 1080, "socks5 proxy server listening port")
	flag.Parse()

	//creating server
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(*port))
	if err != nil {
		slog.Error("Failed to listen on port", "port", *port, "error", err)
	}
	slog.Info("SOCKS5 proxy listening on port", "port", *port)

	//main cycle
	for {
		conn, err := ln.Accept()
		if err != nil {
			slog.Info("Error when accepting connection", "error", err)
			continue
		}
		go handleConn(conn)
	}
}

func handleConn(client net.Conn) {
	defer client.Close()

	// 1) Handshake
	buf := make([]byte, 2)
	if _, err := io.ReadFull(client, buf); err != nil {
		slog.Info("Handshake read error", "error", err)
		return
	}
	if buf[0] != 0x05 {
		slog.Info("Unsupported SOCKS version", "version", buf[0])
		return
	}
	nmethods := int(buf[1])
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(client, methods); err != nil {
		slog.Info("Handshake methods read error", "error", err)
		return
	}

	// Respond: VER=5, METHOD=0x00
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		slog.Info("Handshake reply error", "error", err)
		return
	}

	// 2) Request
	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil {
		slog.Info("Request header read error", "error", err)
		return
	}
	if header[0] != 0x05 {
		slog.Info("Unsupported request version", "version", header[0])
		return
	}
	if header[1] != 0x01 {
		// Only CONNECT supported
		client.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	atyp := header[3]

	// Read DST.ADDR
	var host string
	switch atyp {
	case 0x01: // IPv4
		addr := make([]byte, 4)
		if _, err := io.ReadFull(client, addr); err != nil {
			slog.Info("IPv4 addr read error", "error", err)
			return
		}
		host = net.IP(addr).String()
	case 0x03: // Domain name
		var nameLen [1]byte
		if _, err := io.ReadFull(client, nameLen[:]); err != nil {
			slog.Info("Domain len read error", "error", err)
			return
		}
		domain := make([]byte, nameLen[0])
		if _, err := io.ReadFull(client, domain); err != nil {
			slog.Info("Domain read error", "error", err)
			return
		}
		host = string(domain)
	case 0x04: // IPv6
		addr := make([]byte, 16)
		if _, err := io.ReadFull(client, addr); err != nil {
			slog.Info("IPv6 addr read error", "error", err)
			return
		}
		host = net.IP(addr).String()
	default:
		slog.Info("Unsupported ATYP", "ATYP", atyp)
		client.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0}) // Address type not supported
		return
	}

	// Read DST.PORT
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		slog.Info("Port read error", "error", err)
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	dest := net.JoinHostPort(host, strconv.Itoa(int(port)))

	// 3) CONNECT to destination
	remote, err := net.Dial("tcp", dest)
	if err != nil {
		slog.Info("Dial to dest failed", "destination", dest, "error", err)
		// general failure
		client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	// 4) Reply success
	localAddr := remote.LocalAddr().(*net.TCPAddr)
	bndIP := localAddr.IP
	bndPort := uint16(localAddr.Port)
	resp := []byte{0x05, 0x00, 0x00}
	if ip4 := bndIP.To4(); ip4 != nil {
		resp = append(resp, 0x01)
		resp = append(resp, ip4...)
	} else {
		resp = append(resp, 0x04)
		resp = append(resp, bndIP...)
	}
	portBytes = make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, bndPort)
	resp = append(resp, portBytes...)
	if _, err := client.Write(resp); err != nil {
		slog.Info("Reply write error", "error", err)
		return
	}

	go func() {
		defer client.Close()
		defer remote.Close()
		io.Copy(remote, client)
	}()
	io.Copy(client, remote)
}
