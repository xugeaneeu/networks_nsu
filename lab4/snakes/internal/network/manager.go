package network

import (
	"context"
	"fmt"
	"log"
	"net"
	pb "networks_nsu/lab4/proto"
	"time"

	"google.golang.org/protobuf/proto"
)

const (
	MulticastAddr = "239.192.0.4:9192"
	BufferSize    = 65507
)

type sentMessage struct {
	msg      *pb.GameMessage
	addr     string
	lastSent time.Time
	attempts int
}

// ReceivedMessage содержит данные и адрес отправителя
type ReceivedMessage struct {
	Payload *pb.GameMessage
	Addr    *net.UDPAddr
}

type Manager struct {
	unicastConn   *net.UDPConn
	multicastConn *net.UDPConn
	incomingCh    chan ReceivedMessage
	sentMessages  map[int64]*sentMessage
}

func NewManager() (*Manager, error) {
	// 1. Unicast сокет (биндимся на случайный свободный порт :0)
	// net.ResolveUDPAddr здесь не обязателен для ":0", но полезен для единообразия
	uAddr, err := net.ResolveUDPAddr("udp", ":0")
	if err != nil {
		return nil, fmt.Errorf("resolve unicast: %w", err)
	}
	uConn, err := net.ListenUDP("udp", uAddr)
	if err != nil {
		return nil, fmt.Errorf("listen unicast: %w", err)
	}

	// 2. Multicast сокет
	mAddr, err := net.ResolveUDPAddr("udp", MulticastAddr)
	if err != nil {
		uConn.Close()
		return nil, fmt.Errorf("resolve multicast: %w", err)
	}
	mConn, err := net.ListenMulticastUDP("udp", nil, mAddr)
	if err != nil {
		uConn.Close()
		return nil, fmt.Errorf("listen multicast: %w", err)
	}
	mConn.SetReadBuffer(BufferSize)

	return &Manager{
		unicastConn:   uConn,
		multicastConn: mConn,
		incomingCh:    make(chan ReceivedMessage, 100),
	}, nil
}

// GetLocalAddrString возвращает "IP:Port" текущего узла для отображения или отправки в Announcement
func (m *Manager) GetLocalAddrString() string {
	// Получаем порт, который выдала ОС
	port := m.unicastConn.LocalAddr().(*net.UDPAddr).Port
	return fmt.Sprintf("%s:%d", getOutboundIP(), port)
}

// Events возвращает канал для чтения
func (m *Manager) Events() <-chan ReceivedMessage {
	return m.incomingCh
}

func (m *Manager) Start(ctx context.Context) {
	go m.listenLoop(ctx, m.unicastConn, "unicast")
	go m.listenLoop(ctx, m.multicastConn, "multicast")
}

func (m *Manager) Close() {
	if m.unicastConn != nil {
		m.unicastConn.Close()
	}
	if m.multicastConn != nil {
		m.multicastConn.Close()
	}
	close(m.incomingCh)
}

// SendUnicast отправляет сообщение по строковому адресу (напр. "192.168.1.5:45000")
func (m *Manager) SendUnicast(msg *pb.GameMessage, addrStr string) error {
	// Используем стандартный резолвер
	dst, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return fmt.Errorf("resolve addr %s: %w", addrStr, err)
	}
	return m.send(msg, m.unicastConn, dst)
}

// SendMulticast отправляет сообщение всем
func (m *Manager) SendMulticast(msg *pb.GameMessage) error {
	dst, err := net.ResolveUDPAddr("udp", MulticastAddr)
	if err != nil {
		return err
	}
	// Отправляем через Unicast сокет, чтобы ответ пришел нам
	return m.send(msg, m.unicastConn, dst)
}

func (m *Manager) send(msg *pb.GameMessage, conn *net.UDPConn, addr *net.UDPAddr) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	_, err = conn.WriteToUDP(data, addr)
	return err
}

func (m *Manager) listenLoop(ctx context.Context, conn *net.UDPConn, name string) {
	buf := make([]byte, BufferSize)
	log.Printf("Network: listening %s on %s", name, conn.LocalAddr())

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Устанавливаем дедлайн чтения, чтобы горутина могла проверить ctx.Done()
			// Однако, для простоты (чтобы не спамить в лог ошибками таймаута) оставим блокирующим.
			// При m.Close() ReadFromUDP вернет ошибку и мы выйдем.
			n, srcAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				// Если контекст отменен или сокет закрыт - выходим
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("Network read error (%s): %v", name, err)
					continue // Или return, если ошибка фатальна
				}
			}

			// Копируем данные
			data := make([]byte, n)
			copy(data, buf[:n])

			var msg pb.GameMessage
			if err := proto.Unmarshal(data, &msg); err != nil {
				// Это нормально в UDP - могут прийти битые пакеты или мусор
				continue
			}

			select {
			case m.incomingCh <- ReceivedMessage{Payload: &msg, Addr: srcAddr}:
			case <-ctx.Done():
				return
			default:
				// Дропаем пакет, если канал переполнен (Slow Consumer)
			}
		}
	}
}

// getOutboundIP - приватный хелпер для определения локального IP
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
