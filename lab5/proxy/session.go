package proxy

import "net"

type message interface {
}

type Session struct {
	connClient net.Conn
}

func newSession(conn net.Conn) *Session {
	return &Session{connClient: conn}
}

func (s *Session) Serve() error {

}
