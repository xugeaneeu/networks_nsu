package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
)

var (
	port = flag.Int("port", 9000, "TCP port to listen on")
)

func main() {
	flag.Parse()

	uploadDir := "uploads"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Fatalf("cannot create uploads dir: %v", err)
	}

	addr := fmt.Sprintf(":%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}
	defer listener.Close()
	log.Printf("server listening on %s", addr)

	var wg sync.WaitGroup

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			handleConnection(c, uploadDir)
		}(conn)
	}
}

func handleConnection(conn net.Conn, uploadDir string) {
	defer conn.Close()

	r := bufio.NewReader(conn)

	var nameLen uint16
	if err := binary.Read(r, binary.BigEndian, &nameLen); err != nil {
		log.Printf("[%s] read name length failed: %v", conn.RemoteAddr(), err)
		return
	}

	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		log.Printf("[%s] read filename failed: %v", conn.RemoteAddr(), err)
		return
	}
	filename := string(nameBytes)
	safeName := filepath.Base(filename)

	var fileSize uint64
	if err := binary.Read(r, binary.BigEndian, &fileSize); err != nil {
		log.Printf("[%s] read file size failed: %v", conn.RemoteAddr(), err)
		return
	}

	dstPath := filepath.Join(uploadDir, safeName)
	f, err := os.Create(dstPath)
	if err != nil {
		log.Printf("[%s] cannot create file %q: %v", conn.RemoteAddr(), dstPath, err)
		return
	}
	defer f.Close()

	written, err := io.CopyN(f, r, int64(fileSize))
	if err != nil {
		log.Printf("[%s] write failed after %d bytes: %v", conn.RemoteAddr(), written, err)
		return
	}
	if uint64(written) != fileSize {
		log.Printf("[%s] size mismatch: expected %d, got %d", conn.RemoteAddr(), fileSize, written)
		return
	}

	log.Printf("[%s] received %q (%d bytes) → %s", conn.RemoteAddr(), filename, fileSize, dstPath)
}
