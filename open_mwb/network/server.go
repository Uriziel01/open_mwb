package network

import (
	"fmt"
	"log"
	"net"

	"mwb-linux/crypto"
)

// Server listens for incoming connections from Windows MWB.
type Server struct {
	listener    net.Listener
	mwbCrypto   *crypto.MWBCrypto
	port        int
	machineID   uint32
	machineName string
	debug       bool
}

// NewServer creates a TCP server that listens for MWB connections.
func NewServer(port int, securityKey string, machineID uint32, machineName string, debug bool) (*Server, error) {
	listenPort := port + 1
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		return nil, fmt.Errorf("failed to listen on port %d: %w", listenPort, err)
	}

	log.Printf("[server] Listening on port %d", listenPort)

	return &Server{
		listener:    listener,
		mwbCrypto:   crypto.NewMWBCrypto(securityKey),
		port:        port,
		machineID:   machineID,
		machineName: machineName,
		debug:       debug,
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

		cipher := s.mwbCrypto.NewStreamCipher(s.debug)
		client := &Client{
			Conn:        conn,
			Cipher:      cipher,
			MagicNumber: s.mwbCrypto.MagicNumber,
			MachineID:   s.machineID,
			Debug:       s.debug,
			MachineName: s.machineName,
		}

		// CBC primer: receive first (client sends first), then send
		if err := cipher.ReceiveRandomBlock(conn); err != nil {
			log.Printf("[server] CBC primer receive failed from %s: %v", conn.RemoteAddr(), err)
			conn.Close()
			continue
		}
		if err := cipher.SendRandomBlock(conn); err != nil {
			log.Printf("[server] CBC primer send failed to %s: %v", conn.RemoteAddr(), err)
			conn.Close()
			continue
		}

		// Both Windows and Linux MUST spontaneously emit their 10 handshake packets
		// whether they accepted the tcp connection or initiated it.
		// `client.handshake()` already does exactly this bidirectionally.
		if err := client.handshake(); err != nil {
			log.Printf("[server] Handshake failed from %s: %v", conn.RemoteAddr(), err)
			conn.Close()
			continue
		}

		log.Printf("[server] Handshake successful with %s", conn.RemoteAddr())
		return client, nil
	}
}

// Close stops the server.
func (s *Server) Close() {
	s.listener.Close()
}
