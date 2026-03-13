package network

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"open-mwb/crypto"
	"open-mwb/protocol"
)

type Client struct {
	Conn            net.Conn
	Cipher          *crypto.StreamCipher
	MagicNumber     uint32
	MachineID       uint32
	RemoteMachineID uint32
	Debug           bool
	MachineName     string
	connected       atomic.Bool
	sendMu          sync.Mutex
}

func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

func (c *Client) markDisconnected() {
	c.connected.Store(false)
}

func Connect(address string, port int, securityKey string, machineID uint32, machineName string, debug bool) (*Client, error) {
	connectPort := port + 1

	log.Printf("[connect] Dialing %s:%d...", address, connectPort)
	conn, err := net.DialTimeout("tcp4", fmt.Sprintf("%s:%d", address, connectPort), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to %s:%d: %w", address, connectPort, err)
	}

	mc := crypto.NewMWBCrypto(securityKey)

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
		tcpConn.SetWriteBuffer(32 * 10000)
		tcpConn.SetReadBuffer(32 * 10000)
	}

	c := &Client{
		Conn:        conn,
		Cipher:      mc.NewStreamCipher(debug),
		MagicNumber: mc.MagicNumber,
		MachineID:   machineID,
		Debug:       debug,
		MachineName: machineName,
	}
	// Note: connected flag is set after successful handshake

	if err := c.Cipher.SendRandomBlock(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("CBC primer send: %w", err)
	}
	if err := c.Cipher.ReceiveRandomBlock(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("CBC primer receive: %w", err)
	}

	if err := c.handshake(); err != nil {
		c.Conn.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}

	return c, nil
}

func (c *Client) handshake() error {
	randBuf := make([]byte, 16)
	if _, err := rand.Read(randBuf); err != nil {
		return fmt.Errorf("generate handshake data: %w", err)
	}

	ourM1 := binary.LittleEndian.Uint32(randBuf[0:4])
	ourM2 := binary.LittleEndian.Uint32(randBuf[4:8])
	ourM3 := binary.LittleEndian.Uint32(randBuf[8:12])
	ourM4 := binary.LittleEndian.Uint32(randBuf[12:16])

	handshakeData := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Handshake,
			Id:   1,
			Src:  c.MachineID,
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

	for i := 0; i < 10; i++ {
		encrypted := c.Cipher.Encrypt(plainBytes)
		_, err = c.Conn.Write(encrypted)
		if err != nil {
			return err
		}
	}

	expectedM1 := ^ourM1
	expectedM2 := ^ourM2
	expectedM3 := ^ourM3
	expectedM4 := ^ourM4

	log.Printf("[handshake] Sent 10 packets (name=%q), waiting for exchange...", c.MachineName)

	c.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	gotOurAck := false
	gotTheirHandshake := false

	for !gotOurAck || !gotTheirHandshake {
		pkt, err := c.readPacket()
		if err != nil {
			return fmt.Errorf("handshake: %v", err)
		}

		switch pkt.Header.Type {
		case protocol.Handshake:
			c.RemoteMachineID = pkt.Header.Src

			if pkt.Handshake != nil {
				gotTheirHandshake = true

				ack := &protocol.GenericData{
					Header: protocol.Header{
						Type: protocol.HandshakeAck,
						Id:   pkt.Header.Id,
						Src:  c.MachineID,
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
					return fmt.Errorf("send HandshakeAck: %v", err)
				}
			}

		case protocol.HandshakeAck:
			if pkt.Handshake != nil &&
				pkt.Handshake.Machine1 == expectedM1 &&
				pkt.Handshake.Machine2 == expectedM2 &&
				pkt.Handshake.Machine3 == expectedM3 &&
				pkt.Handshake.Machine4 == expectedM4 {

				c.RemoteMachineID = pkt.Header.Src
				log.Printf("[handshake] Valid HandshakeAck from %q", pkt.MachineName)
				gotOurAck = true
			}
		}
	}

	c.Conn.SetReadDeadline(time.Time{})
	c.connected.Store(true)
	log.Printf("[handshake] Complete - both sides trusted")
	return nil
}

func (c *Client) readPacket() (*protocol.GenericData, error) {
	buf := make([]byte, protocol.PackageSize)
	_, err := io.ReadFull(c.Conn, buf)
	if err != nil {
		return nil, err
	}

	plain := c.Cipher.Decrypt(buf)
	if plain == nil {
		return nil, fmt.Errorf("decrypt failed")
	}

	pktType := protocol.PackageType(plain[0])
	if protocol.IsBigPackage(pktType) {
		buf2 := make([]byte, protocol.PackageSizeEx-protocol.PackageSize)
		_, err = io.ReadFull(c.Conn, buf2)
		if err != nil {
			return nil, fmt.Errorf("read extended packet: %v", err)
		}
		plain2 := c.Cipher.Decrypt(buf2)
		if plain2 != nil {
			plain = append(plain, plain2...)
		}
	}

	return protocol.Unmarshal(plain, c.MagicNumber, c.Debug)
}

func (c *Client) Send(data *protocol.GenericData) error {
	if data.Header.Src == 0 {
		data.Header.Src = c.MachineID
	}
	if data.MachineName == "" && c.MachineName != "" {
		data.MachineName = c.MachineName
	}

	plainBytes, err := protocol.Marshal(data, c.MagicNumber, c.Debug)
	if err != nil {
		return err
	}

	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	encrypted := c.Cipher.Encrypt(plainBytes)
	err = writeAll(c.Conn, encrypted)
	if err != nil {
		c.markDisconnected()
		return err
	}
	return nil
}

func writeAll(conn net.Conn, data []byte) error {
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func (c *Client) Receive() (*protocol.GenericData, error) {
	return c.readPacket()
}
