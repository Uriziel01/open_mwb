package network

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"mwb-linux/crypto"
	"mwb-linux/protocol"
)

// Server listens for incoming connections from Windows MWB.
type Server struct {
	listener net.Listener
	crypto   *crypto.MWBCrypto
	port     int
	machineID uint32
}

// NewServer creates a TCP server that listens for MWB connections.
func NewServer(port int, securityKey string, machineID uint32) (*Server, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	log.Printf("[server] Listening on port %d", port)

	return &Server{
		listener:  listener,
		crypto:    crypto.NewMWBCrypto(securityKey),
		port:      port,
		machineID: machineID,
	}, nil
}

// Accept waits for a single incoming connection, performs the handshake,
// and returns a connected Client.
func (s *Server) Accept() (*Client, error) {
	for {
		log.Println("[server] Waiting for incoming connection...")
		conn, err := s.listener.Accept()
		if err != nil {
			return nil, fmt.Errorf("accept error: %w", err)
		}

		log.Printf("[server] Connection from %s", conn.RemoteAddr())

		client := &Client{
			Conn:   conn,
			Crypto: s.crypto,
		}

		if err := s.handleIncomingHandshake(client); err != nil {
			log.Printf("[server] Handshake failed from %s: %v", conn.RemoteAddr(), err)
			conn.Close()
			continue
		}

		log.Printf("[server] Handshake successful with %s", conn.RemoteAddr())
		return client, nil
	}
}

func (s *Server) handleIncomingHandshake(c *Client) error {
	c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Read encrypted handshake (64 bytes plaintext + PKCS7 pad = 80 bytes ciphertext)
	buf := make([]byte, 80)

	for {
		_, err := io.ReadFull(c.Conn, buf)
		if err != nil {
			return fmt.Errorf("failed to read handshake: %v", err)
		}

		plain := c.Crypto.Decrypt(buf)
		if plain == nil {
			continue
		}

		data, err := protocol.Unmarshal(plain)
		if err != nil {
			continue
		}

		if data.Header.Type == protocol.Handshake && data.Handshake != nil {
			// Respond with HandshakeAck: bitwise NOT of machine fields
			ackData := &protocol.GenericData{
				Header: protocol.Header{
					Type:     protocol.HandshakeAck,
					Id:       data.Header.Id,
					Src:      0, // ID.NONE
					Des:      data.Header.Src,
					DateTime: uint64(time.Now().UnixNano() / 10000),
				},
				Handshake: &protocol.HandshakeData{
					Machine1: ^data.Handshake.Machine1,
					Machine2: ^data.Handshake.Machine2,
					Machine3: ^data.Handshake.Machine3,
					Machine4: ^data.Handshake.Machine4,
				},
			}

			ackBytes, err := protocol.Marshal(ackData)
			if err != nil {
				return fmt.Errorf("failed to marshal ack: %v", err)
			}

			encrypted := c.Crypto.Encrypt(ackBytes)
			_, err = c.Conn.Write(encrypted)
			if err != nil {
				return fmt.Errorf("failed to send ack: %v", err)
			}

			c.Conn.SetReadDeadline(time.Time{})
			return nil
		}
	}
}

// Close stops the server.
func (s *Server) Close() {
	s.listener.Close()
}
