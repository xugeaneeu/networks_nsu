package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
)

var (
	serverAddr = flag.String("addr", "localhost:9000", "server address host:port")
	filePath   = flag.String("file", "", "path to the file to send")
)

func main() {
	flag.Parse()

	if *filePath == "" {
		log.Fatalf("please specify -file")
	}

	f, err := os.Open(*filePath)
	if err != nil {
		log.Fatalf("cannot open file %q: %v", *filePath, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatalf("stat failed: %v", err)
	}
	if fi.IsDir() {
		log.Fatalf("%q is a directory", *filePath)
	}

	fileSize := uint64(fi.Size())
	filename := filepath.Base(*filePath)
	nameBytes := []byte(filename)
	if len(nameBytes) > 0xFFFF {
		log.Fatalf("filename too long: %d bytes", len(nameBytes))
	}
	nameLen := uint16(len(nameBytes))

	conn, err := net.Dial("tcp", *serverAddr)
	if err != nil {
		log.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()
	log.Printf("connected to %s", *serverAddr)

	w := bufio.NewWriter(conn)

	if err := binary.Write(w, binary.BigEndian, nameLen); err != nil {
		log.Fatalf("write name length failed: %v", err)
	}

	if _, err := w.Write(nameBytes); err != nil {
		log.Fatalf("write filename failed: %v", err)
	}

	if err := binary.Write(w, binary.BigEndian, fileSize); err != nil {
		log.Fatalf("write file size failed: %v", err)
	}

	if err := w.Flush(); err != nil {
		log.Fatalf("flush header failed: %v", err)
	}

	sent, err := io.Copy(w, f)
	if err != nil {
		log.Fatalf("sending file content failed after %d bytes: %v", sent, err)
	}
	if uint64(sent) != fileSize {
		log.Fatalf("sent bytes mismatch: expected %d, got %d", fileSize, sent)
	}

	if err := w.Flush(); err != nil {
		log.Fatalf("flush body failed: %v", err)
	}

	log.Printf("sent %q (%d bytes) successfully", filename, fileSize)
}
