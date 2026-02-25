// Package e2e provides end-to-end tests for open-mwb by spinning up two real
// network.Client instances connected over loopback TCP using the full MWB protocol.
package e2e

import (
	"net"
	"testing"
	"time"

	"open-mwb/network"
	"open-mwb/protocol"
)

// ConnectedPair holds both sides of an in-process MWB connection.
type ConnectedPair struct {
	// Client is the side that initiated the outbound TCP connection.
	Client *network.Client
	// Server is the side that accepted the inbound TCP connection.
	Server *network.Client

	srv *network.Server
}

// NewConnectedPair boots a loopback TCP server and client that complete the
// full MWB handshake (CBC primer + Handshake/HandshakeAck) using the given
// security key and machine IDs.
//
// Call pair.Close() when done, or register it with t.Cleanup.
func NewConnectedPair(t *testing.T, key string, clientID, serverID uint32) *ConnectedPair {
	t.Helper()

	const clientName = "test-client"
	const serverName = "test-server"

	// Pick a random free port by letting the OS assign one, then release it.
	// The network.NewServer/Connect constructors add +1 to the port, so we
	// hand them (actualPort - 1).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewConnectedPair: find free port: %v", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	mwbPort := actualPort - 1 // network pkg will do +1 internally

	// readyCh signals that the server has bound its listener and is safe to
	// dial. A non-nil error means NewServer itself failed.
	readyCh := make(chan error, 1)

	type serverResult struct {
		client *network.Client
		srv    *network.Server
		err    error
	}
	ch := make(chan serverResult, 1)

	go func() {
		srv, err := network.NewServer(mwbPort, key, serverID, serverName, false)
		if err != nil {
			readyCh <- err
			return
		}
		readyCh <- nil // listener is up; client may dial now
		c, err := srv.Accept()
		ch <- serverResult{client: c, srv: srv, err: err}
	}()

	// Block until the server is listening (or reports a startup error).
	if err := <-readyCh; err != nil {
		t.Fatalf("NewConnectedPair: server start: %v", err)
		return nil
	}

	// Connect the client side using the real network.Connect (CBC primer + handshake).
	clientConn, err := network.Connect("127.0.0.1", mwbPort, key, clientID, clientName, false)
	if err != nil {
		t.Fatalf("NewConnectedPair: client connect: %v", err)
	}

	// Wait for the server side to finish its handshake.
	select {
	case res := <-ch:
		if res.err != nil {
			clientConn.Conn.Close()
			t.Fatalf("NewConnectedPair: server accept: %v", res.err)
		}
		return &ConnectedPair{
			Client: clientConn,
			Server: res.client,
			srv:    res.srv,
		}
	case <-time.After(10 * time.Second):
		clientConn.Conn.Close()
		t.Fatal("NewConnectedPair: timed out waiting for server handshake")
		return nil
	}
}

// Close shuts down both sides of the pair and the server listener.
func (p *ConnectedPair) Close() {
	if p.Client != nil && p.Client.Conn != nil {
		p.Client.Conn.Close()
	}
	if p.Server != nil && p.Server.Conn != nil {
		p.Server.Conn.Close()
	}
	if p.srv != nil {
		p.srv.Close()
	}
}

// receiveWithTimeout reads packets from a client, skipping any residual
// Handshake/HandshakeAck packets left in the stream after connection setup.
// It fatally fails the test if the deadline is reached or a read error occurs.
func receiveWithTimeout(t *testing.T, c *network.Client, timeout time.Duration) *protocol.GenericData {
	t.Helper()
	type result struct {
		pkt *protocol.GenericData
		err error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			pkt, err := c.Receive()
			if err != nil {
				ch <- result{err: err}
				return
			}
			// Skip residual handshake frames — these can linger in the stream
			// after NewConnectedPair returns because both sides emit 10 handshake
			// packets and the receiver may not have consumed all of them yet.
			if pkt.Header.Type == protocol.Handshake || pkt.Header.Type == protocol.HandshakeAck {
				continue
			}
			ch <- result{pkt: pkt}
			return
		}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("receiveWithTimeout: %v", r.err)
		}
		return r.pkt
	case <-time.After(timeout):
		t.Fatalf("receiveWithTimeout: timed out after %v", timeout)
		return nil
	}
}
