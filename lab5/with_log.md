package main

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"os"
	"strconv"
)

type Buffer struct {
	size     int
	capacity int
	buf      []byte
}

type Session struct {
	conn   net.Conn
	input  *Buffer
	output *Buffer
}

type ProxyServer struct {
	listenPort int
	listener   net.Listener

	sessions []*Session
}

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <port>", os.Args[0])
	}
	port := os.Args[1]
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}
	log.Printf("SOCKS5 proxy listening on :%s", port)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConn(conn)
	}
}

func handleConn(client net.Conn) {
	clientAddr := client.RemoteAddr().String()
	log.Printf("[%s] New connection", clientAddr)
	defer func() {
		log.Printf("[%s] Connection closed", clientAddr)
		client.Close()
	}()

	// 1) Handshake
	log.Printf("[%s] Starting SOCKS5 handshake", clientAddr)
	buf := make([]byte, 2)
	if _, err := io.ReadFull(client, buf); err != nil {
		log.Printf("[%s] Handshake read error: %v", clientAddr, err)
		return
	}
	ver, nmethods := buf[0], int(buf[1])
	log.Printf("[%s] Client version=%d, nmethods=%d", clientAddr, ver, nmethods)
	if ver != 0x05 {
		log.Printf("[%s] Unsupported SOCKS version: %d", clientAddr, ver)
		return
	}
	methods := make([]byte, nmethods)
	if _, err := io.ReadFull(client, methods); err != nil {
		log.Printf("[%s] Handshake methods read error: %v", clientAddr, err)
		return
	}
	log.Printf("[%s] Client methods: %v", clientAddr, methods)
	// Отвечаем NO AUTH
	if _, err := client.Write([]byte{0x05, 0x00}); err != nil {
		log.Printf("[%s] Handshake reply error: %v", clientAddr, err)
		return
	}
	log.Printf("[%s] Sent handshake reply: [0x05, 0x00]", clientAddr)

	// 2) Request
	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil {
		log.Printf("[%s] Request header read error: %v", clientAddr, err)
		return
	}
	log.Printf("[%s] Request header: VER=0x%02X CMD=0x%02X RSV=0x%02X ATYP=0x%02X",
		clientAddr, header[0], header[1], header[2], header[3])
	if header[0] != 0x05 {
		log.Printf("[%s] Unsupported request version: %d", clientAddr, header[0])
		return
	}
	if header[1] != 0x01 {
		log.Printf("[%s] Unsupported command: 0x%02X", clientAddr, header[1])
		client.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	atyp := header[3]

	// Читаем DST.ADDR
	var host string
	switch atyp {
	case 0x01:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(client, addr); err != nil {
			log.Printf("[%s] IPv4 addr read error: %v", clientAddr, err)
			return
		}
		host = net.IP(addr).String()
	case 0x03:
		var nameLen [1]byte
		if _, err := io.ReadFull(client, nameLen[:]); err != nil {
			log.Printf("[%s] Domain len read error: %v", clientAddr, err)
			return
		}
		domain := make([]byte, nameLen[0])
		if _, err := io.ReadFull(client, domain); err != nil {
			log.Printf("[%s] Domain read error: %v", clientAddr, err)
			return
		}
		host = string(domain)
	case 0x04:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(client, addr); err != nil {
			log.Printf("[%s] IPv6 addr read error: %v", clientAddr, err)
			return
		}
		host = net.IP(addr).String()
	default:
		log.Printf("[%s] Unsupported ATYP: 0x%02X", clientAddr, atyp)
		client.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	log.Printf("[%s] Destination host: %s", clientAddr, host)

	// Читаем DST.PORT
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		log.Printf("[%s] Port read error: %v", clientAddr, err)
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	log.Printf("[%s] Destination port: %d", clientAddr, port)
	dest := net.JoinHostPort(host, strconv.Itoa(int(port)))

	// 3) CONNECT to destination
	log.Printf("[%s] Dialing %s", clientAddr, dest)
	remote, err := net.Dial("tcp", dest)
	if err != nil {
		log.Printf("[%s] Dial to %s failed: %v", clientAddr, dest, err)
		client.Write([]byte{0x05, 0x01, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer func() {
		log.Printf("[%s] Remote connection to %s closed", clientAddr, dest)
		remote.Close()
	}()
	log.Printf("[%s] Connected to %s", clientAddr, dest)

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
		log.Printf("[%s] Reply write error: %v", clientAddr, err)
		return
	}
	log.Printf("[%s] Sent connect reply: BND.ADDR=%s BND.PORT=%d", clientAddr, bndIP.String(), bndPort)

	// 5) Relay traffic
	log.Printf("[%s] Starting relay between client and %s", clientAddr, dest)
	// client -> remote
	go func() {
		n, err := io.Copy(remote, client)
		log.Printf("[%s] client->remote: copied %d bytes, err=%v", clientAddr, n, err)
		remote.Close()
		client.Close()
	}()
	// remote -> client
	n, err := io.Copy(client, remote)
	log.Printf("[%s] remote->client: copied %d bytes, err=%v", clientAddr, n, err)
}

