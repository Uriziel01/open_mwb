package network

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"mwb-linux/crypto"
	"mwb-linux/protocol"
)

type Client struct {
	Conn        net.Conn
	Cipher      *crypto.StreamCipher
	MagicNumber uint32
	Debug       bool
	MachineName string
}

func Connect(address string, port int, securityKey string, machineName string, debug bool) (*Client, error) {
	connectPort := port + 1

	log.Printf("[connect] Dialing %s:%d...", address, connectPort)
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", address, connectPort), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MWB at %s:%d: %w", address, connectPort, err)
	}

	mc := crypto.NewMWBCrypto(securityKey)
	c := &Client{
		Conn:        conn,
		Cipher:      mc.NewStreamCipher(debug),
		MagicNumber: mc.MagicNumber,
		Debug:       debug,
		MachineName: machineName,
	}

	if debug {
		log.Printf("[connect] Connected. Magic=0x%08X, Name=%q", mc.MagicNumber, machineName)
	}

	// Prime CBC streams (C#: SendOrReceiveARandomDataBlockPerInitialIV)
	if err := c.Cipher.SendRandomBlock(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("CBC primer send failed: %w", err)
	}
	if err := c.Cipher.ReceiveRandomBlock(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("CBC primer receive failed: %w", err)
	}

	if err := c.handshake(); err != nil {
		c.Conn.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}

	return c, nil
}

func (c *Client) handshake() error {
	// Generate random Machine1-4 (matching C#: buf = RandomNumberGenerator.GetBytes(PACKAGE_SIZE_EX))
	randBuf := make([]byte, 16)
	if _, err := rand.Read(randBuf); err != nil {
		return fmt.Errorf("failed to generate random handshake data: %w", err)
	}

	ourM1 := binary.LittleEndian.Uint32(randBuf[0:4])
	ourM2 := binary.LittleEndian.Uint32(randBuf[4:8])
	ourM3 := binary.LittleEndian.Uint32(randBuf[8:12])
	ourM4 := binary.LittleEndian.Uint32(randBuf[12:16])

	handshakeData := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Handshake,
			Id:   1,
			Src:  0, // ID.NONE during handshake
		},
		Handshake: &protocol.HandshakeData{
			Machine1: ourM1,
			Machine2: ourM2,
			Machine3: ourM3,
			Machine4: ourM4,
		},
		MachineName: c.MachineName,
	}

	plainBytes, err := protocol.Marshal(handshakeData, c.MagicNumber, c.Debug)
	if err != nil {
		return err
	}

	// Send 10 times (each advances CBC state)
	for i := 0; i < 10; i++ {
		encrypted := c.Cipher.Encrypt(plainBytes)
		_, err = c.Conn.Write(encrypted)
		if err != nil {
			return err
		}
	}

	// Compute expected ack values: bitwise NOT of our Machine1-4
	expectedM1 := ^ourM1
	expectedM2 := ^ourM2
	expectedM3 := ^ourM3
	expectedM4 := ^ourM4

	log.Printf("[handshake] Sent 10 handshake packets (name=%q), waiting for handshake exchange...", c.MachineName)

	// Now enter receive loop:
	// - When we get their Handshake: respond with HandshakeAck (bitwise NOT, include our MachineName)
	// - When we get our HandshakeAck (matching our expected values): handshake complete!
	c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	gotOurAck := false

	for !gotOurAck {
		pkt, err := c.readPacket()
		if err != nil {
			return fmt.Errorf("failed during handshake: %v", err)
		}

		switch pkt.Header.Type {
		case protocol.Handshake:
			// Windows sent us its handshake — respond with HandshakeAck
			if pkt.Handshake != nil {
				if c.Debug {
					log.Printf("[handshake] Received their Handshake from %q, sending HandshakeAck", pkt.MachineName)
				}
				ack := &protocol.GenericData{
					Header: protocol.Header{
						Type: protocol.HandshakeAck,
						Id:   pkt.Header.Id,
						Src:  0, // ID.NONE
					},
					Handshake: &protocol.HandshakeData{
						Machine1: ^pkt.Handshake.Machine1,
						Machine2: ^pkt.Handshake.Machine2,
						Machine3: ^pkt.Handshake.Machine3,
						Machine4: ^pkt.Handshake.Machine4,
					},
					MachineName: c.MachineName,
				}

				if err := c.Send(ack); err != nil {
					return fmt.Errorf("failed to send HandshakeAck: %v", err)
				}
			}

		case protocol.HandshakeAck:
			// Check if this is the ack for OUR handshake
			if pkt.Handshake != nil &&
				pkt.Handshake.Machine1 == expectedM1 &&
				pkt.Handshake.Machine2 == expectedM2 &&
				pkt.Handshake.Machine3 == expectedM3 &&
				pkt.Handshake.Machine4 == expectedM4 {

				log.Printf("[handshake] ✓ Received valid HandshakeAck from %q — connection trusted!", pkt.MachineName)
				gotOurAck = true
			} else {
				if c.Debug {
					log.Printf("[handshake] Received HandshakeAck but machine values don't match, skipping")
				}
			}

		default:
			if c.Debug {
				log.Printf("[handshake] Ignoring packet type=%d during handshake", pkt.Header.Type)
			}
		}
	}

	c.Conn.SetReadDeadline(time.Time{})
	return nil
}

// readPacket reads a single packet (base 32 bytes, extended to 64 if big type).
func (c *Client) readPacket() (*protocol.GenericData, error) {
	buf := make([]byte, protocol.PackageSize)
	_, err := io.ReadFull(c.Conn, buf)
	if err != nil {
		return nil, err
	}

	plain := c.Cipher.Decrypt(buf)
	if plain == nil {
		return nil, fmt.Errorf("failed to decrypt base packet")
	}

	pktType := protocol.PackageType(plain[0])
	if protocol.IsBigPackage(pktType) {
		buf2 := make([]byte, protocol.PackageSizeEx-protocol.PackageSize)
		_, err = io.ReadFull(c.Conn, buf2)
		if err != nil {
			return nil, fmt.Errorf("failed to read extended packet: %v", err)
		}
		plain2 := c.Cipher.Decrypt(buf2)
		if plain2 != nil {
			plain = append(plain, plain2...)
		}
	}

	return protocol.Unmarshal(plain, c.MagicNumber, c.Debug)
}

// Send sends a generic data packet encrypted over the TCP socket.
func (c *Client) Send(data *protocol.GenericData) error {
	plainBytes, err := protocol.Marshal(data, c.MagicNumber, c.Debug)
	if err != nil {
		return err
	}

	encrypted := c.Cipher.Encrypt(plainBytes)
	_, err = c.Conn.Write(encrypted)
	return err
}

// Receive blocks until a complete packet is read, decrypted and unmarshaled.
func (c *Client) Receive() (*protocol.GenericData, error) {
	return c.readPacket()
}
