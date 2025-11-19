package proxy

import (
	"errors"
	"log/slog"
	"net"
)

type SocksProxy struct {
	listener net.Listener
}

func NewSocksProxy() *SocksProxy {
	return &SocksProxy{}
}

func (s *SocksProxy) Up(addr string) error {
	var err error
	if s.listener, err = net.Listen("tcp", addr); err != nil {
		slog.Error("fatal error while Up", "error", err)
		return err
	}

	return nil
}

func (s *SocksProxy) Serve() error {
	defer s.listener.Close()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			slog.Error("can't accept while serve", "error", err)
			return err
		}

		go proxy.handleConn(conn)
	}
}
