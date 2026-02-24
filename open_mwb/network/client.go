package network

import (
	"fmt"
	"io"
	"net"
	"time"

	"mwb-linux/crypto"
	"mwb-linux/protocol"
)

type Client struct {
	Conn   net.Conn
	Crypto *crypto.MWBCrypto
}

func Connect(address string, securityKey string) (*Client, error) {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:15100", address), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MWB server at %s: %w", address, err)
	}

	c := &Client{
		Conn:   conn,
		Crypto: crypto.NewMWBCrypto(securityKey),
	}

	if err := c.handshake(); err != nil {
		c.Conn.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}

	return c, nil
}

func (c *Client) handshake() error {
	// 1. Create Handshake packet
	handshakeData := &protocol.GenericData{
		Header: protocol.Header{
			Type:     protocol.Handshake,
			Id:       1,
			Src:      1,                                     // Linux ID
			Des:      0,                                     // Windows ID, 0=None
			DateTime: uint64(time.Now().UnixNano() / 10000), // Windows ticks approx
		},
		Handshake: &protocol.HandshakeData{
			Machine1: 100,
			Machine2: 200,
			Machine3: 300,
			Machine4: 400,
		},
	}

	plainBytes, err := protocol.Marshal(handshakeData)
	if err != nil {
		return err
	}

	encryptedBytes := c.Crypto.Encrypt(plainBytes)

	// 2. Send 10 times consecutively
	for i := 0; i < 10; i++ {
		_, err = c.Conn.Write(encryptedBytes)
		if err != nil {
			return err
		}
	}

	// 3. Receive HandshakeAck (we might get multiple acks because we sent 10)
	c.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	// Usually packets are padded to AES block size (16).
	// Our payload + padding might be 48 or 64 or 80 bytes.
	// Since we know the plaintext was 64 bytes (PackageSizeEx),
	// the PKCS7 padded ciphertext is 80 bytes.
	respBuf := make([]byte, 80)

	// Keep reading until we get a valid HandshakeAck
	for {
		_, err := io.ReadFull(c.Conn, respBuf)
		if err != nil {
			return fmt.Errorf("failed to read handshake ack: %v", err)
		}

		plainResp := c.Crypto.Decrypt(respBuf)
		if plainResp == nil {
			continue // decryption failed, maybe read more bytes or wrong packet?
		}

		respData, err := protocol.Unmarshal(plainResp)
		if err != nil {
			continue
		}

		if respData.Header.Type == protocol.HandshakeAck {
			// 4. Verify bitwise NOT
			if respData.Handshake.Machine1 == ^uint32(100) &&
				respData.Handshake.Machine2 == ^uint32(200) &&
				respData.Handshake.Machine3 == ^uint32(300) &&
				respData.Handshake.Machine4 == ^uint32(400) {

				// Reset deadline
				c.Conn.SetReadDeadline(time.Time{})
				return nil // Handshake success
			} else {
				return fmt.Errorf("handshake ack crypto signature invalid")
			}
		}
	}
}

// Send sends a generic data packet encrypted over the TCP socket
func (c *Client) Send(data *protocol.GenericData) error {
	plainBytes, err := protocol.Marshal(data)
	if err != nil {
		return err
	}

	encrypted := c.Crypto.Encrypt(plainBytes)
	_, err = c.Conn.Write(encrypted)
	return err
}

// Receive blocks until a complete packet is read, decrypted and unmarshaled
func (c *Client) Receive() (*protocol.GenericData, error) {
	// A packet ciphertext length for PackageSizeEx (64) + pkcs7 pad is 80 bytes.
	// For PackageSize (32) + pkcs7 it is 48 bytes.
	// Read a chunk. Easiest way is reading 48 bytes and if it fails, read some more.
	// Since MWB mostly sends fixed size, we could read exactly those.
	buf := make([]byte, 48) // At minimum it's 32 padded to 48
	_, err := io.ReadFull(c.Conn, buf)
	if err != nil {
		return nil, err
	}

	// Wait, MWB packets might be prepended by a 4 byte length header in C# TCP streams,
	// but the analysis document says "fixed-size binary packets", NOT length prefixed.
	// But AES is block based, so if they pad, they send blocks.

	plain := c.Crypto.Decrypt(buf)
	if plain != nil {
		data, err := protocol.Unmarshal(plain)
		if err == nil {
			return data, nil
		}
	}

	// If failed, read up to 80 bytes (32 more)
	buf2 := make([]byte, 32)
	_, err = io.ReadFull(c.Conn, buf2)
	if err != nil {
		return nil, err
	}

	fullBuf := append(buf, buf2...)
	plain = c.Crypto.Decrypt(fullBuf)
	if plain != nil {
		return protocol.Unmarshal(plain)
	}

	return nil, fmt.Errorf("failed to decrypt packet")
}
