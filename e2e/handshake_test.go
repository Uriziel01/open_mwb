package e2e

import (
	"testing"
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
