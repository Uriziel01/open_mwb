package e2e

import (
	"testing"
	"time"

	"open-mwb/protocol"
)

// TestMousePacketRoundtrip sends a Mouse packet from client to server and
// verifies all fields survive the encrypt → decrypt → unmarshal cycle.
func TestMousePacketRoundtrip(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	sent := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   42,
			Src:  testClientID,
			Des:  testServerID,
		},
		Mouse: &protocol.MouseData{
			X:          12345,
			Y:          6789,
			WheelDelta: 3,
			Flags:      int32(protocol.Mouse), // arbitrary flags value
		},
	}

	if err := pair.Client.Send(sent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := receiveWithTimeout(t, pair.Server, 2*time.Second)
	if got == nil {
		t.Fatal("received nil packet")
	}

	if got.Header.Type != protocol.Mouse {
		t.Errorf("Type = %d, want %d", got.Header.Type, protocol.Mouse)
	}
	if got.Header.Id != sent.Header.Id {
		t.Errorf("Id = %d, want %d", got.Header.Id, sent.Header.Id)
	}
	if got.Mouse == nil {
		t.Fatal("Mouse data is nil")
	}
	if got.Mouse.X != sent.Mouse.X {
		t.Errorf("Mouse.X = %d, want %d", got.Mouse.X, sent.Mouse.X)
	}
	if got.Mouse.Y != sent.Mouse.Y {
		t.Errorf("Mouse.Y = %d, want %d", got.Mouse.Y, sent.Mouse.Y)
	}
	if got.Mouse.WheelDelta != sent.Mouse.WheelDelta {
		t.Errorf("Mouse.WheelDelta = %d, want %d", got.Mouse.WheelDelta, sent.Mouse.WheelDelta)
	}
}

// TestKeyboardPacketRoundtrip sends a Keyboard packet and verifies Vk and Flags.
func TestKeyboardPacketRoundtrip(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	const vk = int32(0x41)     // VK_A
	const flags = int32(0x100) // WM_KEYDOWN

	sent := &protocol.GenericData{
		Header: protocol.Header{
			Type:     protocol.Keyboard,
			Id:       7,
			Src:      testClientID,
			Des:      testServerID,
			DateTime: 1234567890,
		},
		Keyboard: &protocol.KeyboardData{
			Vk:    vk,
			Flags: flags,
		},
	}

	if err := pair.Client.Send(sent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := receiveWithTimeout(t, pair.Server, 2*time.Second)
	if got == nil {
		t.Fatal("received nil packet")
	}

	if got.Header.Type != protocol.Keyboard {
		t.Errorf("Type = %d, want %d", got.Header.Type, protocol.Keyboard)
	}
	if got.Keyboard == nil {
		t.Fatal("Keyboard data is nil")
	}
	if got.Keyboard.Vk != vk {
		t.Errorf("Vk = %d, want %d", got.Keyboard.Vk, vk)
	}
	if got.Keyboard.Flags != flags {
		t.Errorf("Flags = %d, want %d", got.Keyboard.Flags, flags)
	}
}

// TestHeartbeatRoundtrip sends a Heartbeat packet (big packet with machine name)
// and verifies the type and machine name survive the round-trip.
func TestHeartbeatRoundtrip(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	const name = "test-client"
	sent := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Heartbeat,
			Id:   99,
			Src:  testClientID,
			Des:  testServerID,
		},
		MachineName: name,
	}

	if err := pair.Client.Send(sent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := receiveWithTimeout(t, pair.Server, 2*time.Second)
	if got == nil {
		t.Fatal("received nil packet")
	}

	if got.Header.Type != protocol.Heartbeat {
		t.Errorf("Type = %d, want %d", got.Header.Type, protocol.Heartbeat)
	}
	if got.MachineName != name {
		t.Errorf("MachineName = %q, want %q", got.MachineName, name)
	}
}

// TestClipboardTextRoundtrip sends a ClipboardText packet and verifies the
// payload survives the round-trip.
//
// Protocol note: ClipboardText is a 64-byte "big" packet. The Raw payload is
// stored at bytes 24-31 (8 bytes) and the MachineName at bytes 32-63. Larger
// clipboard content requires a higher-level chunking protocol (not yet
// implemented). This test validates the fundamental send/receive path.
func TestClipboardTextRoundtrip(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	// Keep text ≤ 8 bytes to stay within the Raw region (bytes 24-31) before
	// the MachineName region starts at byte 32.
	const text = "clipbrd!"
	sent := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.ClipboardText,
			Id:   55,
			Src:  testClientID,
			Des:  testServerID,
		},
		// Suppress auto-fill so MachineName bytes don't overwrite Raw.
		MachineName: " ",
		Raw:         []byte(text),
	}

	if err := pair.Client.Send(sent); err != nil {
		t.Fatalf("Send: %v", err)
	}

	got := receiveWithTimeout(t, pair.Server, 2*time.Second)
	if got == nil {
		t.Fatal("received nil packet")
	}

	if got.Header.Type != protocol.ClipboardText {
		t.Errorf("Type = %d, want %d", got.Header.Type, protocol.ClipboardText)
	}
	if got.Raw == nil {
		t.Fatal("Raw is nil")
	}
	received := string(got.Raw[:len(text)])
	if received != text {
		t.Errorf("Raw text = %q, want %q", received, text)
	}
}

// TestBidirectional verifies that packets flow in both directions independently.
func TestBidirectional(t *testing.T) {
	pair := NewConnectedPair(t, testKey, testClientID, testServerID)
	t.Cleanup(pair.Close)

	// Client → Server
	toServer := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   1,
			Src:  testClientID,
			Des:  testServerID,
		},
		Mouse: &protocol.MouseData{X: 100, Y: 200},
	}

	// Server → Client
	toClient := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   2,
			Src:  testServerID,
			Des:  testClientID,
		},
		Mouse: &protocol.MouseData{X: 300, Y: 400},
	}

	// Send both directions concurrently.
	errs := make(chan error, 2)
	go func() { errs <- pair.Client.Send(toServer) }()
	go func() { errs <- pair.Server.Send(toClient) }()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Errorf("Send error: %v", err)
		}
	}

	// Receive on both sides.
	fromClient := receiveWithTimeout(t, pair.Server, 2*time.Second)
	if fromClient == nil || fromClient.Mouse == nil {
		t.Fatal("server: bad packet from client")
	}
	if fromClient.Mouse.X != 100 || fromClient.Mouse.Y != 200 {
		t.Errorf("server received X=%d Y=%d, want X=100 Y=200", fromClient.Mouse.X, fromClient.Mouse.Y)
	}

	fromServer := receiveWithTimeout(t, pair.Client, 2*time.Second)
	if fromServer == nil || fromServer.Mouse == nil {
		t.Fatal("client: bad packet from server")
	}
	if fromServer.Mouse.X != 300 || fromServer.Mouse.Y != 400 {
		t.Errorf("client received X=%d Y=%d, want X=300 Y=400", fromServer.Mouse.X, fromServer.Mouse.Y)
	}
}
