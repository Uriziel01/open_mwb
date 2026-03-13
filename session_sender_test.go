package main

import (
	"sync"
	"testing"

	"open-mwb/protocol"
)

type fakePacketClient struct {
	mu        sync.Mutex
	connected bool
	packets   []*protocol.GenericData
}

func (f *fakePacketClient) Send(data *protocol.GenericData) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	pktCopy := *data
	f.packets = append(f.packets, &pktCopy)
	return nil
}

func (f *fakePacketClient) IsConnected() bool {
	return f.connected
}

func TestSessionSenderMonotonicIDsAcrossMixedTraffic(t *testing.T) {
	client := &fakePacketClient{connected: true}
	sender := newSessionSender(client, 111, 222)

	const (
		nMouse     = 40
		nKeyboard  = 40
		nHeartbeat = 40
		total      = nMouse + nKeyboard + nHeartbeat
	)

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < nMouse; i++ {
			sender.Send(protocol.Mouse, &protocol.MouseData{X: int32(i), Y: int32(i), Flags: 0x0200})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < nKeyboard; i++ {
			sender.Send(protocol.Keyboard, &protocol.KeyboardData{Vk: 0x41, Flags: 0})
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < nHeartbeat; i++ {
			sender.Send(protocol.Heartbeat, nil)
		}
	}()

	wg.Wait()

	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.packets) != total {
		t.Fatalf("packets sent = %d, want %d", len(client.packets), total)
	}

	for i, pkt := range client.packets {
		wantID := uint32(i + 1)
		if pkt.Header.Id != wantID {
			t.Fatalf("packet index %d has id=%d, want %d", i, pkt.Header.Id, wantID)
		}
		if pkt.Header.Src != 111 || pkt.Header.Des != 222 {
			t.Fatalf("packet index %d has src/des=%d/%d, want 111/222", i, pkt.Header.Src, pkt.Header.Des)
		}
	}
}
