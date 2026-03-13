package main

import (
	"log"
	"sync"
	"time"

	"open-mwb/protocol"
)

type packetClient interface {
	Send(data *protocol.GenericData) error
	IsConnected() bool
}

// sessionSender serializes outbound packets and shares one monotonically
// increasing packet ID sequence across all message types in a session.
type sessionSender struct {
	client packetClient
	srcID  uint32
	dstID  uint32

	mu     sync.Mutex
	nextID uint32
}

func newSessionSender(client packetClient, srcID, dstID uint32) *sessionSender {
	return &sessionSender{
		client: client,
		srcID:  srcID,
		dstID:  dstID,
	}
}

func (s *sessionSender) Send(pktType protocol.PackageType, payload interface{}) {
	if !s.client.IsConnected() {
		return
	}

	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type:     pktType,
			Src:      s.srcID,
			Des:      s.dstID,
			DateTime: uint64(time.Now().UnixNano() / 10000),
		},
	}

	switch v := payload.(type) {
	case *protocol.MouseData:
		pkt.Mouse = v
	case *protocol.KeyboardData:
		pkt.Keyboard = v
	case []byte:
		pkt.Raw = v
	}

	s.mu.Lock()
	s.nextID++
	pkt.Header.Id = s.nextID
	err := s.client.Send(pkt)
	s.mu.Unlock()
	if err != nil && s.client.IsConnected() {
		log.Printf("[send] Failed to send %v: %v", pktType, err)
	}
}
