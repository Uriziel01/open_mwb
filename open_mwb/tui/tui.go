package tui

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"mwb-linux/input"
	"mwb-linux/network"
	"mwb-linux/protocol"
)

// Screen represents the virtual terminal screen for debugging.
type Screen struct {
	mu sync.Mutex

	// Virtual screen dimensions (in terminal cells)
	Width  int
	Height int

	// Cursor position
	CursorX int
	CursorY int

	// Edge config
	Edge string

	// State
	IsRemote bool
	Status   string

	// Network
	Client          *network.Client
	MachineID       uint32
	RemoteMachineID uint32
	PacketID        uint32

	// Original terminal state for restore
	origTermios unix.Termios
}

// New creates a new TUI debug screen.
func New(width, height int, edge string, client *network.Client, machineID, remoteMachineID uint32, debug bool) *Screen {
	_ = debug // reserved for future use
	return &Screen{
		Width:           width,
		Height:          height,
		CursorX:         width / 2,
		CursorY:         height / 2,
		Edge:            edge,
		Client:          client,
		MachineID:       machineID,
		RemoteMachineID: remoteMachineID,
		PacketID:        100,
		Status:          "LOCAL - use arrows to move, hit edge to switch",
	}
}

// Run starts the TUI loop. This blocks until 'q' is pressed.
func (s *Screen) Run() {
	s.enableRawMode()
	defer s.disableRawMode()

	// Hide real cursor and clear screen
	fmt.Print("\033[?25l") // hide cursor
	fmt.Print("\033[2J")   // clear screen
	defer fmt.Print("\033[?25h\033[2J") // restore on exit

	// Start receive loop
	go s.receiveLoop()

	// Start heartbeat
	go s.heartbeatLoop()

	s.render()

	// Read input
	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		if buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 3 { // q or Ctrl+C
			return
		}

		// Arrow keys come as ESC [ A/B/C/D
		if n == 3 && buf[0] == 27 && buf[1] == '[' {
			dx, dy := 0, 0
			switch buf[2] {
			case 'A': // Up
				dy = -1
			case 'B': // Down
				dy = 1
			case 'C': // Right
				dx = 1
			case 'D': // Left
				dx = -1
			}

			if dx != 0 || dy != 0 {
				s.handleArrowKey(dx, dy)
			}
		}

		// Space to manually toggle back to local
		if buf[0] == ' ' && s.IsRemote {
			s.mu.Lock()
			s.IsRemote = false
			s.Status = "LOCAL - returned via spacebar"
			s.CursorX = s.Width / 2
			s.CursorY = s.Height / 2
			s.mu.Unlock()
			s.render()
		}
	}
}

func (s *Screen) handleArrowKey(dx, dy int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRemote {
		// Forward movement to Windows as relative mouse events
		s.sendMousePacket(dx*5, dy*5, 0, input.WinMouseEventFMove)
		// Also move local cursor to show direction
		s.CursorX += dx
		s.CursorY += dy
		s.clampCursor()
		s.renderLocked()
		return
	}

	// Local mode: move cursor and check edge
	s.CursorX += dx
	s.CursorY += dy

	// Check edge before clamping
	edgeHit := false
	switch s.Edge {
	case "right":
		edgeHit = s.CursorX >= s.Width-1
	case "left":
		edgeHit = s.CursorX <= 0
	case "top":
		edgeHit = s.CursorY <= 0
	case "bottom":
		edgeHit = s.CursorY >= s.Height-1
	}

	s.clampCursor()

	if edgeHit {
		s.IsRemote = true
		s.Status = fmt.Sprintf("REMOTE - controlling Windows! (space=return)")
		s.renderLocked()
		return
	}

	s.renderLocked()
}

func (s *Screen) clampCursor() {
	if s.CursorX < 0 {
		s.CursorX = 0
	}
	if s.CursorX >= s.Width {
		s.CursorX = s.Width - 1
	}
	if s.CursorY < 0 {
		s.CursorY = 0
	}
	if s.CursorY >= s.Height {
		s.CursorY = s.Height - 1
	}
}

func (s *Screen) render() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renderLocked()
}

func (s *Screen) renderLocked() {
	var b strings.Builder

	// Move to top-left
	b.WriteString("\033[H")

	// Title
	modeColor := "\033[32m" // green = local
	modeLabel := "LOCAL"
	if s.IsRemote {
		modeColor = "\033[31m" // red = remote
		modeLabel = "REMOTE"
	}
	b.WriteString(fmt.Sprintf(" %s⬤ %s\033[0m  MWB Debug TUI  |  Edge: %s  |  Cursor: (%d,%d)\n",
		modeColor, modeLabel, s.Edge, s.CursorX, s.CursorY))

	// Top border
	b.WriteString(" ┌")
	b.WriteString(strings.Repeat("─", s.Width))
	b.WriteString("┐\n")

	// Screen area
	for y := 0; y < s.Height; y++ {
		b.WriteString(" │")
		for x := 0; x < s.Width; x++ {
			if x == s.CursorX && y == s.CursorY {
				if s.IsRemote {
					b.WriteString("\033[31m█\033[0m") // red cursor in remote mode
				} else {
					b.WriteString("\033[32m█\033[0m") // green cursor in local mode
				}
			} else {
				// Draw edge indicators
				isEdge := false
				switch s.Edge {
				case "right":
					isEdge = x == s.Width-1
				case "left":
					isEdge = x == 0
				case "top":
					isEdge = y == 0
				case "bottom":
					isEdge = y == s.Height-1
				}
				if isEdge {
					b.WriteString("\033[33m·\033[0m") // yellow dots for the active edge
				} else {
					b.WriteByte(' ')
				}
			}
		}
		b.WriteString("│\n")
	}

	// Bottom border
	b.WriteString(" └")
	b.WriteString(strings.Repeat("─", s.Width))
	b.WriteString("┘\n")

	// Status line
	b.WriteString(fmt.Sprintf(" %s\033[K\n", s.Status))
	b.WriteString(" [arrows]=move  [space]=return to local  [q]=quit\033[K\n")

	fmt.Print(b.String())
}

func (s *Screen) sendMousePacket(dx, dy, wheelDelta, flags int) {
	s.PacketID++
	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   s.PacketID,
			Src:  s.MachineID,
			Des:  s.RemoteMachineID,
		},
		Mouse: &protocol.MouseData{
			X:          int32(dx),
			Y:          int32(dy),
			WheelDelta: int32(wheelDelta),
			Flags:      int32(flags),
		},
	}

	if err := s.Client.Send(pkt); err != nil {
		s.Status = fmt.Sprintf("Send error: %v", err)
	}
}

func (s *Screen) receiveLoop() {
	for {
		pkt, err := s.Client.Receive()
		if err != nil {
			s.mu.Lock()
			s.Status = fmt.Sprintf("Recv error: %v", err)
			s.mu.Unlock()
			s.render()
			time.Sleep(time.Second)
			continue
		}

		s.mu.Lock()
		switch pkt.Header.Type {
		case protocol.Mouse:
			if pkt.Mouse != nil {
				if pkt.Mouse.Flags&int32(input.WinMouseEventFAbsolute) != 0 {
					// Absolute: scale from 0-65535 to our screen
					s.CursorX = int(pkt.Mouse.X) * s.Width / 65536
					s.CursorY = int(pkt.Mouse.Y) * s.Height / 65536
				} else {
					// Relative movement
					s.CursorX += int(pkt.Mouse.X) / 5
					s.CursorY += int(pkt.Mouse.Y) / 5
				}
				s.clampCursor()
				s.Status = fmt.Sprintf("Recv mouse: (%d,%d) flags=0x%X", pkt.Mouse.X, pkt.Mouse.Y, pkt.Mouse.Flags)

				// If Windows is sending us input, we're being controlled
				if !s.IsRemote {
					s.IsRemote = false // We're receiving, so we're the target (local display)
					s.Status = fmt.Sprintf("INCOMING: Windows cursor at (%d,%d)", s.CursorX, s.CursorY)
				}
			}

		case protocol.Keyboard:
			if pkt.Keyboard != nil {
				action := "DOWN"
				if pkt.Keyboard.Flags&int32(input.WinKeyEventFKeyUp) != 0 {
					action = "UP"
				}
				s.Status = fmt.Sprintf("Recv key: VK=0x%X %s", pkt.Keyboard.Vk, action)
			}

		case protocol.Heartbeat:
			// silent

		case protocol.ClipboardText:
			if pkt.Raw != nil {
				text := string(pkt.Raw)
				if len(text) > 40 {
					text = text[:40] + "..."
				}
				s.Status = fmt.Sprintf("Recv clipboard: %q", text)
			}

		default:
			s.Status = fmt.Sprintf("Recv pkt type=%d", pkt.Header.Type)
		}
		s.renderLocked()
		s.mu.Unlock()
	}
}

func (s *Screen) heartbeatLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		s.PacketID++
		pkt := &protocol.GenericData{
			Header: protocol.Header{
				Type:     protocol.Heartbeat,
				Id:       s.PacketID,
				Src:      s.MachineID,
				DateTime: uint64(time.Now().UnixNano() / 10000),
			},
		}
		s.Client.Send(pkt)
		s.mu.Unlock()
	}
}

func (s *Screen) enableRawMode() {
	fd := int(os.Stdin.Fd())
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		log.Fatalf("Failed to get terminal state: %v", err)
	}
	s.origTermios = *termios

	raw := *termios
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		log.Fatalf("Failed to set raw mode: %v", err)
	}
}

func (s *Screen) disableRawMode() {
	fd := int(os.Stdin.Fd())
	unix.IoctlSetTermios(fd, unix.TCSETS, &s.origTermios)
}
