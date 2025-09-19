package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"sort"
	"time"

	"golang.org/x/net/ipv6"
)

func main() {
	groupFlag := flag.String("group", "", "multicast group address with port, e.g. 224.0.0.1:9999 or [ff02::1]:9999")
	intervalFlag := flag.Duration("interval", 2*time.Second, "heartbeat send interval")
	timeoutFlag := flag.Duration("timeout", 5*time.Second, "peer disappearence timeout")
	flag.Parse()

	if *groupFlag == "" {
		log.Fatal("Please specify -group, e.g. -group 224.0.0.1:9999")
	}

	udpAddr, err := net.ResolveUDPAddr("udp", *groupFlag)
	if err != nil {
		log.Fatalf("Failed to resolve multicast address %q: %v", *groupFlag, err)
	}

	var proto string
	if udpAddr.IP.To4() != nil {
		proto = "IPv4"
	} else {
		proto = "IPv6"
	}

	fmt.Println("Multicast group:", udpAddr.String())
	fmt.Println("Protocol:", proto)
	fmt.Println("Heartbeat interval:", *intervalFlag)
	fmt.Println("Peer timeout:", *timeoutFlag)

	ifaces, err := net.Interfaces()
	if err != nil {
		log.Fatalf("net.Interfaces failed: %v", err)
	}

	var goodIfaces []net.Interface
	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp == 0) || (iface.Flags&net.FlagMulticast == 0) {
			continue
		}
		goodIfaces = append(goodIfaces, iface)
	}

	fmt.Println("Will use interfaces:")
	for _, iface := range goodIfaces {
		fmt.Println(" ", iface.Name)
	}

	msgCh := make(chan string, 100)

	if proto == "IPv4" {
		for _, ifi := range goodIfaces {
			conn, err := net.ListenMulticastUDP("udp4", &ifi, udpAddr)
			if err != nil {
				log.Println("Join4:", ifi.Name, err)
				continue
			}
			conn.SetReadBuffer(1 << 20)
			go reader4(conn, msgCh)
		}
	} else {
		pc, err := net.ListenPacket("udp6", fmt.Sprintf("[::]:%d", udpAddr.Port))
		if err != nil {
			log.Fatal("listenPacket6: ", err)
		}
		p := ipv6.NewPacketConn(pc)
		for _, ifi := range goodIfaces {
			if err := p.JoinGroup(&ifi, udpAddr); err != nil {
				log.Fatal("Join6: ", ifi.Name, err)
			}
		}
		go reader6(pc, msgCh)
	}

	go sender(proto, udpAddr, *intervalFlag)

	peers := make(map[string]time.Time)
	cleanup := time.NewTicker(*timeoutFlag / 2)
	defer cleanup.Stop()

	for {
		select {
		case peerID := <-msgCh:
			now := time.Now()
			if _, ex := peers[peerID]; !ex {
				fmt.Println("New peer live: ", peerID)
			}
			peers[peerID] = now

		case <-cleanup.C:
			now := time.Now()
			changed := false
			for peerID, last := range peers {
				if now.Sub(last) > *timeoutFlag {
					fmt.Println("Peer died: ", peerID)
					delete(peers, peerID)
					changed = true
				}
			}
			if changed {
				keys := make([]string, 0, len(peers))
				for k := range peers {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				fmt.Println("Currently live peers: ")
				for _, k := range keys {
					fmt.Println(" ", k)
				}
			}
		}
	}
}

func reader4(conn *net.UDPConn, ch chan<- string) {
	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Println("reader4: ", err)
			return
		}
		if n > 0 {
			ch <- src.String()
		}
	}
}

func reader6(pc net.PacketConn, ch chan<- string) {
	buf := make([]byte, 1500)
	for {
		n, src, err := pc.ReadFrom(buf)
		if err != nil {
			log.Println("reader6: ", err)
			return
		}
		if n > 0 {
			if udp, ok := src.(*net.UDPAddr); ok {
				ch <- udp.String()
			}
		}
	}
}

func sender(proto string, group *net.UDPAddr, interval time.Duration) {
	netw := "udp4"
	if proto == "IPv6" {
		netw = "udp6"
	}

	conn, err := net.DialUDP(netw, nil, group)
	if err != nil {
		log.Fatalf("sender Dial: %v", err)
	}
	defer conn.Close()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	msg := []byte("ping")
	for range ticker.C {
		if _, err := conn.Write(msg); err != nil {
			log.Println("sender write:", err)
		}
	}
}
