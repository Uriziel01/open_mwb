package e2e

import (
	"net"
	"testing"
	"time"

	"open-mwb/network"
)

const (
	testKey      = "TestSecurityKey123"
	testClientID = uint32(1001)
	testServerID = uint32(2002)
)

// TestHandshakeCompletes verifies that two peers can complete the full MWB
// handshake (CBC primer + Handshake/HandshakeAck) over loopback TCP.
func TestHandshakeCompletes(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	// Both sides must have captured the remote machine ID during the handshake.
	if pair.Client.RemoteMachineID != testServerID {
		t.Errorf("client.RemoteMachineID = %d, want %d", pair.Client.RemoteMachineID, testServerID)
	}
	if pair.Server.RemoteMachineID != testClientID {
		t.Errorf("server.RemoteMachineID = %d, want %d", pair.Server.RemoteMachineID, testClientID)
	}
}

// TestHandshakeMachineNames verifies that machine names are exchanged correctly.
func TestHandshakeMachineNames(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	if pair.Client.MachineName != "test-client" {
		t.Errorf("client.MachineName = %q, want %q", pair.Client.MachineName, "test-client")
	}
	if pair.Server.MachineName != "test-server" {
		t.Errorf("server.MachineName = %q, want %q", pair.Server.MachineName, "test-server")
	}
}

// TestWrongKeyRejected verifies that a client connecting with a mismatched
// security key cannot complete the handshake. The magic-number or checksum
// validation in Unmarshal must reject the corrupted packets, causing Connect
// to return a non-nil error.
func TestWrongKeyRejected(t *testing.T) {
	// Grab a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	mwbPort := actualPort - 1

	// Start a server with the correct key.
	readyCh := make(chan error, 1)
	go func() {
		srv, err := network.NewServer(mwbPort, testKey, testServerID, "test-server", false)
		if err != nil {
			readyCh <- err
			return
		}
		readyCh <- nil // listening
		// Accept (and discard) the connection attempt; the server loop will
		// return an error from the failed handshake and continue waiting, so
		// we just close the server to unblock it.
		srv.Accept() //nolint:errcheck
		srv.Close()
	}()

	if err := <-readyCh; err != nil {
		t.Fatalf("server start: %v", err)
	}

	// Give the server goroutine a moment to reach Accept before we dial.
	// (This is a tiny window needed only because Accept blocks synchronously.)
	time.Sleep(5 * time.Millisecond)

	// Dial with a wrong key — the handshake must fail.
	const wrongKey = "WrongSecurityKey!"
	_, err = network.Connect("127.0.0.1", mwbPort, wrongKey, testClientID, "bad-client", false)
	if err == nil {
		t.Fatal("Connect with wrong key should have returned an error")
	}
}
