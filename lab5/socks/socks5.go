package socks

const (
	Ver          = 0x05
	MethodNoAuth = 0x00
	CmdConnect   = 0x01

	AtypIPv4       = 0x01
	AtypDomainName = 0x03

	RepSuccess              = 0x00
	RepGenFailure           = 0x01
	RepConnectionNotAllowed = 0x02
	RepNetworkUnreachable   = 0x03
	RepHostUnreachable      = 0x04
	RepConnectionRefused    = 0x05
	RepTTLExpired           = 0x06
	RepCmdNotSupported      = 0x07
	RepAddrTypeNotSupported = 0x08
)

var (
	MethodNoAuthConst = MethodNoAuth
	CmdConnectConst   = CmdConnect
	RepSuccessConst   = RepSuccess
)

type Socks5HandshakeRequest struct {
	Version  byte
	NMethods byte
	Methods  []byte
}

type Socks5HandshakeReply struct {
	Version byte
	Method  byte
}

type Socks5Request struct {
	Version  byte
	Cmd      byte
	AddrType byte
	Addr     string
	Port     uint16
}

type Socks5Reply struct {
	Version  byte
	Rep      byte
	AddrType byte
	Addr     string
	Port     uint16
}

// func ReadMethodSelection(r io.Reader) ([]byte, error) {
// 	header := make([]byte, 2)

// 	if _, err := io.ReadFull(r, header); err != nil {
// 		return nil, err
// 	}
// 	if header[0] != Ver {
// 		return nil, errors.New("unsupported socks version")
// 	}

// 	methods := make([]byte, header[1])
// 	if header[1] > 0 {
// 		if _, err := io.ReadFull(r, methods); err != nil {
// 			return nil, err
// 		}
// 	}

// 	return methods, nil
// }

// func WriteMethodChoice(w io.Writer, method byte) error {
// 	_, err := w.Write([]byte{Ver, method})
// 	return err
// }
