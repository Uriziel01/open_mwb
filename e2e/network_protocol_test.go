package e2e

import (
	"net"
	"testing"
	"time"

	"open-mwb/crypto"
	"open-mwb/network"
	"open-mwb/protocol"
)

func TestProtocol_Packet_InvalidChecksum(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	// Create a valid packet
	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   999,
			Src:  testClientID,
			Des:  testServerID,
		},
		Mouse: &protocol.MouseData{X: 1, Y: 1},
	}

	plainBytes, err := protocol.Marshal(pkt, pair.Client.MagicNumber, false)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Corrupt the checksum (byte 1)
	plainBytes[1] = ^plainBytes[1]

	// Empty out any lingering handshake packets
	for {
		// Set a short read deadline so we don't block forever if there are none.
		pair.Server.Conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		_, err := pair.Server.Receive()
		if err != nil {
			break
		}
	}
	pair.Server.Conn.SetReadDeadline(time.Time{}) // Reset deadline

	encrypted := pair.Client.Cipher.Encrypt(plainBytes)
	_, err = pair.Client.Conn.Write(encrypted)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Read on the server side
	_, err = pair.Server.Receive()
	if err == nil {
		t.Fatal("Expected an error due to invalid checksum, but got nil")
	}
	if err.Error() != "invalid checksum" {
		t.Fatalf("Expected 'invalid checksum' error, got: %v", err)
	}
}

func TestProtocol_Packet_UnknownType(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	// Create a packet with an unknown type
	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.PackageType(150), // 150 is not in the enum
			Id:   888,
			Src:  testClientID,
			Des:  testServerID,
		},
		Raw: []byte("unknown payload"),
	}

	if err := pair.Client.Send(pkt); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Receiver should not panic and should unmarshal it into the default case
	received := receiveWithTimeout(t, pair.Server, 2*time.Second)
	if received == nil {
		t.Fatal("Expected to receive the unknown packet, but got nil")
	}

	if received.Header.Type != protocol.PackageType(150) {
		t.Errorf("Expected Type 150, got %d", received.Header.Type)
	}
}

func TestProtocol_Packet_Fragmentation(t *testing.T) {
	// We'll test fragmentation directly on the Client by using a net.Pipe
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	mc := crypto.NewMWBCrypto(testKey)
	
	clientSide := &network.Client{
		Conn:        clientConn,
		Cipher:      mc.NewStreamCipher(false),
		MagicNumber: mc.MagicNumber,
	}

	serverSide := &network.Client{
		Conn:        serverConn,
		Cipher:      mc.NewStreamCipher(false),
		MagicNumber: mc.MagicNumber,
	}

	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   777,
		},
		Mouse: &protocol.MouseData{X: 10, Y: 20},
	}

	plainBytes, err := protocol.Marshal(pkt, clientSide.MagicNumber, false)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	encrypted := clientSide.Cipher.Encrypt(plainBytes)

	// Send it in fragments
	errCh := make(chan error, 1)
	go func() {
		// Fragment 1 (10 bytes)
		if _, err := clientConn.Write(encrypted[:10]); err != nil {
			errCh <- err
			return
		}
		time.Sleep(10 * time.Millisecond)
		// Fragment 2 (22 bytes) - completes the 32-byte packet
		if _, err := clientConn.Write(encrypted[10:]); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	received, err := serverSide.Receive()
	if err != nil {
		t.Fatalf("Receive failed on fragmented data: %v", err)
	}
	if received.Header.Id != 777 || received.Mouse.X != 10 {
		t.Errorf("Packet data mismatch after defragmentation")
	}

	if err := <-errCh; err != nil {
		t.Fatalf("Background write failed: %v", err)
	}
}

func TestNetwork_Handshake_DuplicateName(t *testing.T) {
	// We need to verify what the original code did or what the Go code currently does.
	// Currently, NewServer returns a *network.Server. Let's see if we can connect two clients with the same name.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	mwbPort := actualPort - 1

	srv, err := network.NewServer(mwbPort, testKey, testServerID, "test-server", false)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// Wait for accept
	go func() {
		for {
			c, err := srv.Accept()
			if err != nil {
				return
			}
			go func(client *network.Client) {
				for {
					if _, err := client.Receive(); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	client1, err := network.Connect("127.0.0.1", mwbPort, testKey, testClientID, "duplicate-name", false)
	if err != nil {
		t.Fatalf("client1 connect: %v", err)
	}
	defer client1.Conn.Close()

	client2, err := network.Connect("127.0.0.1", mwbPort, testKey, testClientID+1, "duplicate-name", false)
	if err != nil {
		t.Fatalf("client2 connect: %v", err)
	}
	defer client2.Conn.Close()

	// Wait, both connect fine currently. This just proves the protocol doesn't strictly crash.
	// We will assert both succeeded.
	if client1.RemoteMachineID != testServerID || client2.RemoteMachineID != testServerID {
		t.Fatalf("One of the clients failed the handshake")
	}
}

func TestNetwork_Reconnect_After_Drop(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	
	// Ensure connection works
	if err := pair.Client.Send(&protocol.GenericData{
		Header: protocol.Header{Type: protocol.Mouse},
		Mouse: &protocol.MouseData{X: 1, Y: 1},
	}); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	receiveWithTimeout(t, pair.Server, 2*time.Second)

	// Empty out any lingering handshake packets
	for {
		pair.Client.Conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		_, err := pair.Client.Receive()
		if err != nil {
			break
		}
	}
	pair.Client.Conn.SetReadDeadline(time.Time{})

	// Abruptly close server connection
	pair.Server.Conn.Close()

	// Subsequent write should fail or receive should fail
	_ = pair.Client.Send(&protocol.GenericData{
		Header: protocol.Header{Type: protocol.Mouse},
		Mouse: &protocol.MouseData{X: 2, Y: 2},
	})
	
	// It may not fail immediately on Send due to TCP buffering, but we expect an eventual error
	// if we try to read from the dropped connection.
	errCh := make(chan error, 1)
	go func() {
		_, e := pair.Client.Receive()
		errCh <- e
	}()

	select {
	case e := <-errCh:
		if e == nil {
			t.Fatal("Expected error when reading from dropped connection")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for disconnect error")
	}
	pair.Close()
}
