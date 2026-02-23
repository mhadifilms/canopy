// Package stun implements a minimal STUN server (RFC 5389) for NAT traversal.
// It responds to Binding Requests with the client's reflexive transport address.
package stun

import (
	"encoding/binary"
	"errors"
	"net"

	"go.uber.org/zap"
)

const (
	// STUN message types.
	bindingRequest  = 0x0001
	bindingResponse = 0x0101

	// STUN attribute types.
	attrXORMappedAddress = 0x0020

	// STUN magic cookie.
	magicCookie = 0x2112A442

	// Header size: 20 bytes (type 2 + length 2 + magic 4 + txn id 12).
	headerSize = 20
)

// Server is a STUN server that responds to Binding Requests.
type Server struct {
	conn   net.PacketConn
	logger *zap.Logger
	done   chan struct{}
}

// New creates a new STUN server.
func New(logger *zap.Logger) *Server {
	return &Server{
		logger: logger,
		done:   make(chan struct{}),
	}
}

// ListenAndServe starts the STUN server on the given address (e.g., ":3478").
func (s *Server) ListenAndServe(addr string) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	s.conn = conn
	s.logger.Info("stun server started", zap.String("addr", addr))

	go s.serve()
	return nil
}

// Addr returns the listen address, useful for tests with ":0" port.
func (s *Server) Addr() net.Addr {
	if s.conn == nil {
		return nil
	}
	return s.conn.LocalAddr()
}

// Close shuts down the STUN server.
func (s *Server) Close() error {
	close(s.done)
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *Server) serve() {
	buf := make([]byte, 1500)

	for {
		select {
		case <-s.done:
			return
		default:
		}

		n, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.logger.Error("stun read error", zap.Error(err))
				continue
			}
		}

		if n < headerSize {
			continue
		}

		msgType := binary.BigEndian.Uint16(buf[0:2])
		if msgType != bindingRequest {
			continue
		}

		// Extract transaction ID (bytes 8-20).
		var txnID [12]byte
		copy(txnID[:], buf[8:20])

		resp, err := buildBindingResponse(txnID, addr)
		if err != nil {
			s.logger.Error("stun build response error", zap.Error(err))
			continue
		}

		if _, err := s.conn.WriteTo(resp, addr); err != nil {
			s.logger.Error("stun write error", zap.Error(err))
		}
	}
}

// buildBindingResponse creates a STUN Binding Response with XOR-MAPPED-ADDRESS.
func buildBindingResponse(txnID [12]byte, addr net.Addr) ([]byte, error) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return nil, errors.New("not a UDP address")
	}

	ip4 := udpAddr.IP.To4()
	if ip4 == nil {
		return nil, errors.New("only IPv4 supported")
	}

	// Build XOR-MAPPED-ADDRESS attribute.
	// Family: 0x01 (IPv4), Port XOR'd with magic cookie upper 16 bits,
	// IP XOR'd with magic cookie.
	xorPort := uint16(udpAddr.Port) ^ uint16(magicCookie>>16)

	var xorIP [4]byte
	var magicBytes [4]byte
	binary.BigEndian.PutUint32(magicBytes[:], magicCookie)
	for i := 0; i < 4; i++ {
		xorIP[i] = ip4[i] ^ magicBytes[i]
	}

	// Attribute: type (2) + length (2) + reserved (1) + family (1) + port (2) + ip (4) = 12 bytes.
	attrLen := 8 // 1 reserved + 1 family + 2 port + 4 ip
	attr := make([]byte, 4+attrLen)
	binary.BigEndian.PutUint16(attr[0:2], attrXORMappedAddress)
	binary.BigEndian.PutUint16(attr[2:4], uint16(attrLen))
	attr[4] = 0x00 // reserved
	attr[5] = 0x01 // IPv4
	binary.BigEndian.PutUint16(attr[6:8], xorPort)
	copy(attr[8:12], xorIP[:])

	// Build response header.
	resp := make([]byte, headerSize+len(attr))
	binary.BigEndian.PutUint16(resp[0:2], bindingResponse)
	binary.BigEndian.PutUint16(resp[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(resp[4:8], magicCookie)
	copy(resp[8:20], txnID[:])
	copy(resp[20:], attr)

	return resp, nil
}

// ParseBindingResponse extracts the XOR-MAPPED-ADDRESS from a STUN Binding Response.
// Returns the IP and port. Useful for clients and testing.
func ParseBindingResponse(data []byte) (ip net.IP, port int, err error) {
	if len(data) < headerSize {
		return nil, 0, errors.New("response too short")
	}

	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != bindingResponse {
		return nil, 0, errors.New("not a binding response")
	}

	attrLen := binary.BigEndian.Uint16(data[2:4])
	attrs := data[headerSize : headerSize+int(attrLen)]

	for len(attrs) >= 4 {
		aType := binary.BigEndian.Uint16(attrs[0:2])
		aLen := binary.BigEndian.Uint16(attrs[2:4])

		if int(aLen)+4 > len(attrs) {
			break
		}

		if aType == attrXORMappedAddress && aLen >= 8 {
			family := attrs[5]
			if family != 0x01 { // IPv4 only
				attrs = attrs[4+aLen:]
				continue
			}

			xorPort := binary.BigEndian.Uint16(attrs[6:8])
			port = int(xorPort ^ uint16(magicCookie>>16))

			var magicBytes [4]byte
			binary.BigEndian.PutUint32(magicBytes[:], magicCookie)
			ip = make(net.IP, 4)
			for i := 0; i < 4; i++ {
				ip[i] = attrs[8+i] ^ magicBytes[i]
			}
			return ip, port, nil
		}

		attrs = attrs[4+aLen:]
	}

	return nil, 0, errors.New("XOR-MAPPED-ADDRESS not found")
}

// BuildBindingRequest creates a minimal STUN Binding Request.
func BuildBindingRequest(txnID [12]byte) []byte {
	req := make([]byte, headerSize)
	binary.BigEndian.PutUint16(req[0:2], bindingRequest)
	binary.BigEndian.PutUint16(req[2:4], 0) // no attributes
	binary.BigEndian.PutUint32(req[4:8], magicCookie)
	copy(req[8:20], txnID[:])
	return req
}
