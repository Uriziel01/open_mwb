package tui

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"open-mwb/input"
	"open-mwb/network"
	"open-mwb/protocol"
	"open-mwb/util"
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

	RemoteCursorX int32
	RemoteCursorY int32

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
	origState *term.State
}

// New creates a new TUI debug screen.
func New(edge string, client *network.Client, machineID, remoteMachineID uint32, debug bool) *Screen {
	_ = debug // reserved for future use

	// Get actual terminal size
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		// Fallback to reasonable defaults
		width, height = 80, 24
	}

	// Account for UI elements around the content area:
	// Title (1) + Top border (1) + Content (Height) + Bottom border (1) + Status lines (2) = Height + 5
	// Leave extra room for terminal chrome/prompt
	contentWidth := width - 4    // Left border (2 chars) + right border (2 chars)
	contentHeight := height - 20 // Title + borders + status + padding + terminal chrome

	if contentWidth < 18 {
		contentWidth = 18
	}
	if contentHeight < 8 {
		contentHeight = 8
	}

	return &Screen{
		Width:           contentWidth,
		Height:          contentHeight,
		CursorX:         contentWidth / 2,
		CursorY:         contentHeight / 2,
		Edge:            edge,
		Client:          client,
		MachineID:       machineID,
		RemoteMachineID: remoteMachineID,
		PacketID:        100,
		RemoteCursorX:   32768,
		RemoteCursorY:   32768,
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

		// 'x' to click
		if (buf[0] == 'x' || buf[0] == 'X') && s.IsRemote {
			s.mu.Lock()
			s.sendMousePacket(int(s.RemoteCursorX), int(s.RemoteCursorY), 0, input.WM_LBUTTONDOWN)
			s.sendMousePacket(int(s.RemoteCursorX), int(s.RemoteCursorY), 0, input.WM_LBUTTONUP)
			s.Status = "REMOTE - left click sent"
			s.mu.Unlock()
			s.render()
		}

		// 'c' to copy timestamp to target PC clipboard
		if (buf[0] == 'c' || buf[0] == 'C') && s.IsRemote {
			s.mu.Lock()
			formatted := formatClipboardText(time.Now().Format(time.RFC3339))
			
			// Send clipboard data in chunks (48 bytes per packet for ClipboardText)
			chunkSize := 48
			for i := 0; i < len(formatted); i += chunkSize {
				end := i + chunkSize
				if end > len(formatted) {
					end = len(formatted)
				}
				chunk := formatted[i:end]
				s.PacketID++
				pkt := &protocol.GenericData{
					Header: protocol.Header{
						Type:     protocol.ClipboardText,
						Id:       s.PacketID,
						Src:      s.MachineID,
						Des:      s.RemoteMachineID,
						DateTime: uint64(time.Now().UnixNano() / 10000),
					},
					Raw: chunk,
				}
				s.Client.Send(pkt)
			}
			
			// Always send ClipboardDataEnd to signal end of transfer
			s.PacketID++
			endPkt := &protocol.GenericData{
				Header: protocol.Header{
					Type:     protocol.ClipboardDataEnd,
					Id:       s.PacketID,
					Src:      s.MachineID,
					Des:      s.RemoteMachineID,
					DateTime: uint64(time.Now().UnixNano() / 10000),
				},
			}
			if err := s.Client.Send(endPkt); err != nil {
				s.Status = "Send error: " + err.Error()
			} else {
				s.Status = "REMOTE - sent clipboard timestamp"
			}
			s.mu.Unlock()
			s.render()
		}
	}
}

func (s *Screen) handleArrowKey(dx, dy int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRemote {
		// Forward movement to Windows as absolute mouse events
		s.RemoteCursorX += int32(dx * 1000)
		s.RemoteCursorY += int32(dy * 1000)
		
		if s.RemoteCursorX < 0 { s.RemoteCursorX = 0 }
		if s.RemoteCursorX > 65535 { s.RemoteCursorX = 65535 }
		if s.RemoteCursorY < 0 { s.RemoteCursorY = 0 }
		if s.RemoteCursorY > 65535 { s.RemoteCursorY = 65535 }

		s.sendMousePacket(int(s.RemoteCursorX), int(s.RemoteCursorY), 0, input.WM_MOUSEMOVE)
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
		s.RemoteCursorX = 32768
		s.RemoteCursorY = 32768
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

	// Clear screen and move to top-left to prevent old frame artifacts
	b.WriteString("\033[2J\033[H")

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
	b.WriteString(" [arrows]=move  [space]=return to local  [x]=click  [c]=clipboard  [q]=quit\033[K\n")

	fmt.Print(b.String())
}

func (s *Screen) sendMousePacket(x, y, wheelDelta, flags int) {
	s.PacketID++
	
	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   s.PacketID,
			Src:  s.MachineID,
			Des:  s.RemoteMachineID,
		},
		Mouse: &protocol.MouseData{
			X:          int32(x),
			Y:          int32(y),
			WheelDelta: int32(wheelDelta),
			Flags:      int32(flags),
		},
	}

	if err := s.Client.Send(pkt); err != nil {
		s.Status = fmt.Sprintf("Send error: %v", err)
	}
}

func (s *Screen) receiveLoop() {
	var clipboardBuffer []byte
	var receivingClipboard bool

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
				// We always receive Absolute coordinates from Windows. Scale them directly to TUI grid.
				s.CursorX = int(pkt.Mouse.X) * s.Width / 65536
				s.CursorY = int(pkt.Mouse.Y) * s.Height / 65536
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
				clipboardBuffer = append(clipboardBuffer, pkt.Raw...)
				receivingClipboard = true
				s.Status = fmt.Sprintf("Recv clipboard chunk: %d bytes (total: %d)", len(pkt.Raw), len(clipboardBuffer))
			}

		case protocol.ClipboardDataEnd:
			if receivingClipboard && len(clipboardBuffer) > 0 {
				text := decompressAndParseClipboard(clipboardBuffer)
				if len(text) > 40 {
					text = text[:40] + "..."
				}
				s.Status = fmt.Sprintf("Recv clipboard: %q", text)
				clipboardBuffer = nil
				receivingClipboard = false
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
			MachineName: s.Client.MachineName,
		}
		s.Client.Send(pkt)
		s.mu.Unlock()
	}
}

// decompressAndParseClipboard decompresses and decodes clipboard data from Windows
// Windows MWB sends clipboard data as: UTF-16 LE text, optionally compressed with flate
// Format: "TXT<payload>{GUID}"
func decompressAndParseClipboard(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Try to decompress first (Windows MWB compresses larger text)
	var decompressed []byte
	if len(data) > 10 {
		r := flate.NewReader(bytes.NewReader(data))
		var err error
		decompressed, err = io.ReadAll(r)
		r.Close()
		if err != nil {
			// Not compressed, use raw data
			decompressed = data
		}
	} else {
		decompressed = data
	}

	// Decode UTF-16 LE to UTF-8
	var text string
	if len(decompressed) >= 2 && decompressed[1] == 0 {
		// Likely UTF-16 LE (alternate bytes are 0 for ASCII)
		runes := make([]rune, 0, len(decompressed)/2)
		for i := 0; i < len(decompressed)-1; i += 2 {
			r := uint16(decompressed[i]) | (uint16(decompressed[i+1]) << 8)
			if r != 0 {
				runes = append(runes, rune(r))
			}
		}
		text = string(runes)
	} else {
		// Already UTF-8
		text = string(decompressed)
	}

	// Parse the MWB format: remove "TXT" prefix and "{GUID}" suffix
	payload, err := util.ParseMWBClipboardFormat(text)
	if err != nil {
		// If parsing fails, return the raw text (might be a different format)
		return text
	}

	return payload
}

// formatClipboardText formats text for Windows MWB compatibility
// Format: "TXT<payload>{GUID}"
// Encoded as UTF-16 LE
// Note: Only compress if data exceeds 48 bytes (packet limit), otherwise send raw UTF-16
func formatClipboardText(text string) []byte {
	// Use the helper function to generate the MWB-formatted clipboard string
	formatted := util.GenerateMWBClipboardFormat(text)

	// Encode as UTF-16 LE
	utf16Bytes := make([]byte, len(formatted)*2)
	for i, r := range formatted {
		utf16Bytes[i*2] = byte(r)
		utf16Bytes[i*2+1] = byte(r >> 8)
	}

	// Only compress if it exceeds packet limit (48 bytes for clipboard data)
	// For small text, compression adds overhead and may exceed the limit
	if len(utf16Bytes) > 48 {
		var buf bytes.Buffer
		w, _ := flate.NewWriter(&buf, flate.DefaultCompression)
		w.Write(utf16Bytes)
		w.Close()
		return buf.Bytes()
	}

	// Return raw UTF-16 for small text
	return utf16Bytes
}

func (s *Screen) enableRawMode() {
	fd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		log.Fatalf("Failed to get terminal state: %v", err)
	}
	s.origState = state
}

func (s *Screen) disableRawMode() {
	if s.origState != nil {
		term.Restore(int(os.Stdin.Fd()), s.origState)
	}
}
