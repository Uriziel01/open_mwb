package network

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"open-mwb/crypto"
	"open-mwb/protocol"
)

func TestClientSendConcurrentDoesNotCorruptStream(t *testing.T) {
	senderConn, receiverConn := net.Pipe()
	defer senderConn.Close()
	defer receiverConn.Close()

	mwb := crypto.NewMWBCrypto("test-concurrent-send-key")

	sender := &Client{
		Conn:        senderConn,
		Cipher:      mwb.NewStreamCipher(false),
		MagicNumber: mwb.MagicNumber,
		MachineID:   1001,
		MachineName: "sender",
	}
	receiver := &Client{
		Conn:        receiverConn,
		Cipher:      mwb.NewStreamCipher(false),
		MagicNumber: mwb.MagicNumber,
		MachineID:   2002,
		MachineName: "receiver",
	}
	sender.connected.Store(true)
	receiver.connected.Store(true)

	const (
		workers      = 16
		packetsPerGo = 40
		totalPackets = workers * packetsPerGo
	)

	var nextID atomic.Uint32
	errCh := make(chan error, totalPackets+1)
	receivedIDs := make(chan uint32, totalPackets)
	recvDone := make(chan struct{})

	go func() {
		defer close(recvDone)
		for i := 0; i < totalPackets; i++ {
			_ = receiver.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			pkt, err := receiver.Receive()
			if err != nil {
				errCh <- fmt.Errorf("receive packet %d: %w", i+1, err)
				return
			}
			if pkt.Header.Type != protocol.Mouse || pkt.Mouse == nil {
				errCh <- fmt.Errorf("packet %d malformed: type=%v mouseNil=%v", i+1, pkt.Header.Type, pkt.Mouse == nil)
				return
			}
			receivedIDs <- pkt.Header.Id
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < packetsPerGo; j++ {
				id := nextID.Add(1)
				pkt := &protocol.GenericData{
					Header: protocol.Header{
						Type: protocol.Mouse,
						Id:   id,
						Src:  sender.MachineID,
						Des:  receiver.MachineID,
					},
					Mouse: &protocol.MouseData{
						X:     int32(id),
						Y:     int32(id * 2),
						Flags: 0x0200, // WM_MOUSEMOVE
					},
				}
				if err := sender.Send(pkt); err != nil {
					errCh <- fmt.Errorf("send id=%d: %w", id, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	select {
	case <-recvDone:
	case <-time.After(8 * time.Second):
		t.Fatal("timeout waiting for receiver")
	}

	select {
	case err := <-errCh:
		t.Fatalf("stream corruption under concurrent send: %v", err)
	default:
	}

	close(receivedIDs)
	seen := make(map[uint32]bool, totalPackets)
	for id := range receivedIDs {
		if seen[id] {
			t.Fatalf("duplicate packet id received: %d", id)
		}
		seen[id] = true
	}
	if len(seen) != totalPackets {
		t.Fatalf("received %d packets, want %d", len(seen), totalPackets)
	}
}
