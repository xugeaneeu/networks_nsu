// server.go
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
	"sync/atomic"
	"time"
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
		log.Printf("[%s] failed to read name length: %v", conn.RemoteAddr(), err)
		return
	}
	nameBytes := make([]byte, nameLen)
	if _, err := io.ReadFull(r, nameBytes); err != nil {
		log.Printf("[%s] failed to read filename: %v", conn.RemoteAddr(), err)
		return
	}
	filename := string(nameBytes)
	safeName := filepath.Base(filename)

	var fileSize uint64
	if err := binary.Read(r, binary.BigEndian, &fileSize); err != nil {
		log.Printf("[%s] failed to read file size: %v", conn.RemoteAddr(), err)
		return
	}

	dstPath := filepath.Join(uploadDir, safeName)
	f, err := os.Create(dstPath)
	if err != nil {
		log.Printf("[%s] cannot create file %q: %v", conn.RemoteAddr(), dstPath, err)
		return
	}
	defer f.Close()

	var totalRead uint64
	start := time.Now()
	lastTime := start
	var lastBytes uint64

	ticker := time.NewTicker(3 * time.Second)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				tb := atomic.LoadUint64(&totalRead)
				delta := tb - lastBytes
				dt := now.Sub(lastTime).Seconds()
				if dt > 0 {
					inst := float64(delta) / dt
					avg := float64(tb) / now.Sub(start).Seconds()
					log.Printf("[%s] instant: %.2f B/s, average: %.2f B/s", conn.RemoteAddr(), inst, avg)
				}
				lastTime = now
				lastBytes = tb
			case <-done:
				now := time.Now()
				tb := atomic.LoadUint64(&totalRead)
				if now.Sub(lastTime) < 3*time.Second {
					delta := tb - lastBytes
					dt := now.Sub(lastTime).Seconds()
					if dt <= 0 {
						delta = tb
						dt = now.Sub(start).Seconds()
					}
					inst := float64(delta) / dt
					avg := float64(tb) / now.Sub(start).Seconds()
					log.Printf("[%s] instant: %.2f B/s, average: %.2f B/s", conn.RemoteAddr(), inst, avg)
				}
				return
			}
		}
	}()

	success := true
	left := fileSize
	buf := make([]byte, 32*1024)
	for left > 0 {
		n, err := r.Read(buf)
		if n > 0 {
			toWrite := n
			if uint64(toWrite) > left {
				toWrite = int(left)
			}
			if wn, werr := f.Write(buf[:toWrite]); werr != nil {
				log.Printf("[%s] write error: %v", conn.RemoteAddr(), werr)
				success = false
				break
			} else {
				atomic.AddUint64(&totalRead, uint64(wn))
				left -= uint64(wn)
			}
		}
		if err != nil {
			if err == io.EOF && left == 0 {
				break
			}
			log.Printf("[%s] read error: %v", conn.RemoteAddr(), err)
			success = false
			break
		}
	}

	ticker.Stop()
	close(done)

	if success && atomic.LoadUint64(&totalRead) != fileSize {
		log.Printf("[%s] size mismatch: expected %d, got %d", conn.RemoteAddr(), fileSize, atomic.LoadUint64(&totalRead))
		success = false
	}

	var resp byte
	if success {
		log.Printf("[%s] received %q (%d bytes) → %s", conn.RemoteAddr(), filename, fileSize, dstPath)
		resp = 1
	} else {
		log.Printf("[%s] failed to receive %q", conn.RemoteAddr(), filename)
		resp = 0
	}
	if _, err := conn.Write([]byte{resp}); err != nil {
		log.Printf("[%s] failed to send response: %v", conn.RemoteAddr(), err)
	}
	log.Printf("[%s] connection closed", conn.RemoteAddr())
}
