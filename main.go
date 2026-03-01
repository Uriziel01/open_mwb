package main

import (
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"open-mwb/clipboard"
	"open-mwb/config"
	"open-mwb/input"
	"open-mwb/network"
	"open-mwb/protocol"
	"open-mwb/tui"
	"open-mwb/util"
)

const Version = "0.1.0"

func main() {
	cfg := config.Parse()

	if cfg.ShowVersion {
		fmt.Printf("open-mwb version %s\n", Version)
		os.Exit(0)
	}

	if cfg.ListDevices {
		fmt.Println("Available input devices:")
		input.ListDevices()
		os.Exit(0)
	}

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		config.PrintUsage()
		os.Exit(1)
	}

	if cfg.Demo {
		runDemo(cfg)
		os.Exit(0)
	}

	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Printf("=== open-mwb v%s ===", Version)
	log.Printf("Mode: %s | Edge: %s | Screen: %dx%d | MachineID: %d",
		cfg.Mode, cfg.Edge, cfg.ScreenWidth, cfg.ScreenHeight, cfg.MachineID)

	if cfg.Mode == "tui" {
		// TUI mode doesn't support reconnection - run once
		client, err := connectClient(cfg)
		if err != nil {
			log.Fatalf("Connection failed: %v", err)
		}
		screen := tui.New(cfg.Edge, client, cfg.MachineID, client.RemoteMachineID, cfg.Debug)
		screen.Run()
		client.Conn.Close()
		return
	}

	// Main reconnection loop
	for {
		client, err := connectClient(cfg)
		if err != nil {
			log.Printf("Connection failed: %v", err)
			log.Printf("Retrying in 5 seconds...")
			time.Sleep(5 * time.Second)
			continue
		}

		// Create cancellation context for this connection
		ctx, cancel := context.WithCancel(context.Background())
		
		// Run the main session
		disconnectCh := runSession(ctx, cfg, client)

		// Wait for either disconnection or interrupt signal
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		
		select {
		case <-sigCh:
			// User interrupted - clean shutdown
			log.Println("Shutting down...")
			cancel()
			// Give goroutines time to stop before closing connection
			time.Sleep(100 * time.Millisecond)
			client.Conn.Close()
			return
		case <-disconnectCh:
			// Connection lost - reconnect
			log.Printf("Connection lost, reconnecting in 5 seconds...")
			cancel()
			// Give goroutines time to stop before closing connection
			time.Sleep(100 * time.Millisecond)
			client.Conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

// runSession starts all goroutines for a connected session and returns a channel 
// that signals when the connection is lost
func runSession(ctx context.Context, cfg *config.Config, client *network.Client) <-chan struct{} {
	disconnectCh := make(chan struct{})

	vi, err := input.NewVirtualInput(cfg.ScreenWidth, cfg.ScreenHeight)
	if err != nil {
		log.Fatalf("Failed to create virtual input: %v", err)
	}

	evdev := setupInputCapture(cfg, client)

	clip := clipboard.New()
	setupClipboard(clip, client, cfg)

	go evdev.RunMouseLoop()
	go evdev.RunKeyboardLoop()
	go clip.Watch()
	go sendHeartbeats(ctx, client, cfg)
	go receiveLoop(ctx, client, vi, clip, evdev, cfg.Debug, disconnectCh)
	go emergencyKillSwitch(ctx, evdev)

	log.Println("")
	log.Println("Ready! Move your mouse to the screen edge to switch.")
	log.Println("Press ScrollLock to return input to this machine.")
	log.Println("Press F1 for emergency kill (releases all devices).")
	log.Println("Press Ctrl+C to quit.")

	// Wait for disconnection or context cancellation, then cleanup
	go func() {
		select {
		case <-ctx.Done():
		case <-disconnectCh:
		}
		// Cleanup
		evdev.Close()
		clip.Stop()
		vi.Close()
	}()

	return disconnectCh
}

func connectClient(cfg *config.Config) (*network.Client, error) {
	if cfg.Mode == "client" || cfg.Mode == "tui" {
		server, err := network.NewServer(cfg.ListenPort, cfg.SecurityKey, cfg.MachineID, cfg.MachineName, cfg.Debug)
		if err != nil {
			log.Printf("Warning: Background server failed to start: %v", err)
		} else {
			go runBackgroundServer(server)
		}
	}

	switch cfg.Mode {
	case "client", "tui":
		log.Printf("Connecting to Windows MWB at %s:%d...", cfg.RemoteAddress, cfg.ListenPort+1)
		client, err := network.Connect(cfg.RemoteAddress, cfg.ListenPort, cfg.SecurityKey, cfg.MachineID, cfg.MachineName, cfg.Debug)
		if err != nil {
			return nil, err
		}
		log.Println("Connected and handshake complete!")
		return client, nil

	case "server":
		log.Printf("Starting server on port %d...", cfg.ListenPort+1)
		server, err := network.NewServer(cfg.ListenPort, cfg.SecurityKey, cfg.MachineID, cfg.MachineName, cfg.Debug)
		if err != nil {
			return nil, fmt.Errorf("server start failed: %w", err)
		}
		defer server.Close()

		client, err := server.Accept()
		if err != nil {
			return nil, fmt.Errorf("accept failed: %w", err)
		}
		log.Println("Windows MWB connected!")
		return client, nil

	default:
		return nil, fmt.Errorf("unknown mode: %s", cfg.Mode)
	}
}

func runBackgroundServer(server *network.Server) {
	defer server.Close()
	for {
		client, err := server.Accept()
		if err != nil {
			log.Printf("Background server accept error: %v", err)
			return
		}
		log.Printf("Accepted reciprocal connection from %s", client.MachineName)
		go func(c *network.Client) {
			defer c.Conn.Close()
			for {
				_, err := c.Receive()
				if err != nil {
					return
				}
			}
		}(client)
	}
}

func setupInputCapture(cfg *config.Config, client *network.Client) *input.EvdevCapture {
	evdev := input.NewEvdevCapture(cfg.ScreenWidth, cfg.ScreenHeight, cfg.Edge)

	// Use auto-discovery by default (recommended - works with all hardware)
	if cfg.MouseDevice == "" && cfg.KeyboardDevice == "" {
		log.Println("Discovering input devices by capability...")
		if err := evdev.DiscoverAndOpen(); err != nil {
			log.Fatalf("Failed to discover input devices: %v", err)
		}
	} else {
		// Fallback to specific device paths if configured
		mouseDev := cfg.MouseDevice
		if mouseDev == "" {
			var err error
			mouseDev, err = input.FindMouseDevice()
			if err != nil {
				log.Fatalf("Failed to find mouse: %v", err)
			}
		}

		kbdDev := cfg.KeyboardDevice
		if kbdDev == "" {
			var err error
			kbdDev, err = input.FindKeyboardDevice()
			if err != nil {
				log.Fatalf("Failed to find keyboard: %v", err)
			}
		}

		log.Printf("Using mouse: %s", mouseDev)
		log.Printf("Using keyboard: %s", kbdDev)
	}

	log.Printf("Screen: %dx%d, Edge: %s", cfg.ScreenWidth, cfg.ScreenHeight, cfg.Edge)

	var sendMu sync.Mutex
	packetID := uint32(100)
	nextID := func() uint32 {
		sendMu.Lock()
		defer sendMu.Unlock()
		packetID++
		return packetID
	}

	cursorX, cursorY := int32(32768), int32(32768)

	evdev.OnEdgeHit = func() {
		log.Println("[main] Edge hit - forwarding to remote")
	}

	evdev.OnReturn = func() {
		log.Println("[main] Returning to local")
	}

	evdev.OnMouseEvent = func(dx, dy, wheel int) {
		if wheel == 0 {
			cursorX += int32(dx * 40)
			cursorY += int32(dy * 40)
			cursorX = clamp(cursorX, 0, 65535)
			cursorY = clamp(cursorY, 0, 65535)
		}

		flags := input.WM_MOUSEMOVE
		if wheel != 0 {
			flags = input.WM_MOUSEWHEEL
		}

		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Mouse, sendMu,
			&protocol.MouseData{X: cursorX, Y: cursorY, WheelDelta: int32(wheel), Flags: int32(flags)})
	}

	evdev.OnButtonEvent = func(code uint16, pressed bool) {
		flags := buttonFlags(code, pressed)
		if flags == 0 {
			return
		}
		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Mouse, sendMu,
			&protocol.MouseData{X: cursorX, Y: cursorY, Flags: int32(flags)})
	}

	evdev.OnKeyEvent = func(code uint16, pressed bool) {
		vk, ok := input.LinuxToVK[code]
		if !ok {
			log.Printf("[KEYBOARD] Linux code %d -> NO MAPPING", code)
			return
		}

		flags := int32(0)
		action := "DOWN"
		if !pressed {
			flags = input.WinKeyEventFKeyUp
			action = "UP"
		}

		log.Printf("[KEYBOARD] Linux code %d -> VK 0x%02X (%s)", code, vk, action)

		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Keyboard, sendMu,
			&protocol.KeyboardData{Vk: vk, Flags: flags})
	}

	return evdev
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

func setupClipboard(clip *clipboard.Clipboard, client *network.Client, cfg *config.Config) {
	var sendMu sync.Mutex
	packetID := uint32(100)

	nextID := func() uint32 {
		sendMu.Lock()
		defer sendMu.Unlock()
		packetID++
		return packetID
	}

	clip.OnChange = func(content string) {
		formatted := formatClipboardText(content)
		log.Printf("[clipboard] Sending %d chars (formatted: %d bytes)", len(content), len(formatted))
		log.Printf("[clipboard] Data preview: %q", content[:min(len(content), 50)])

		// Send clipboard data in chunks (48 bytes per packet for ClipboardText)
		chunkSize := 48
		for i := 0; i < len(formatted); i += chunkSize {
			end := i + chunkSize
			if end > len(formatted) {
				end = len(formatted)
			}
			chunk := formatted[i:end]
			sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.ClipboardText, sendMu, chunk)
		}

		// Always send ClipboardDataEnd to signal end of transfer
		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.ClipboardDataEnd, sendMu, nil)
		log.Printf("[clipboard] Sent ClipboardDataEnd marker")
	}
}

func sendPacket(client *network.Client, id, src, dst uint32, pktType protocol.PackageType, mu sync.Mutex, payload interface{}) {
	if !client.IsConnected() {
		return
	}

	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type:     pktType,
			Id:       id,
			Src:      src,
			Des:      dst,
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

	mu.Lock()
	err := client.Send(pkt)
	mu.Unlock()
	if err != nil {
		// Only log errors if we're still connected (not during intentional shutdown)
		if client.IsConnected() {
			log.Printf("[send] Failed to send %v: %v", pktType, err)
		}
	}
}

func sendHeartbeats(ctx context.Context, client *network.Client, cfg *config.Config) {
	var sendMu sync.Mutex
	packetID := uint32(100)

	nextID := func() uint32 {
		sendMu.Lock()
		defer sendMu.Unlock()
		packetID++
		return packetID
	}

	for i := 0; i < 15; i++ {
		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Heartbeat, sendMu, nil)
		time.Sleep(100 * time.Millisecond)
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Heartbeat, sendMu, nil)
		}
	}
}

func receiveLoop(ctx context.Context, client *network.Client, vi *input.VirtualInput, clip *clipboard.Clipboard, evdev *input.EvdevCapture, debug bool, disconnectCh chan<- struct{}) {
	var clipboardBuffer []byte
	var receivingClipboard bool

	for {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return
		default:
		}

		pkt, err := client.Receive()
		if err != nil {
			if err == io.EOF {
				log.Printf("[recv] Connection closed by remote (EOF)")
				close(disconnectCh)
				return
			}
			log.Printf("[recv] Error: %v", err)
			continue
		}

		switch pkt.Header.Type {
		case protocol.Mouse:
			if pkt.Mouse != nil {
				vi.InjectMouse(pkt.Mouse.X, pkt.Mouse.Y, pkt.Mouse.WheelDelta, pkt.Mouse.Flags)
			}

		case protocol.Keyboard:
			if pkt.Keyboard != nil {
				vi.InjectKeyboard(pkt.Keyboard.Vk, pkt.Keyboard.Flags)
			}

		case protocol.ClipboardText:
			if pkt.Raw != nil {
				clipboardBuffer = append(clipboardBuffer, pkt.Raw...)
				receivingClipboard = true
				if debug {
					log.Printf("[recv] Clipboard chunk: %d bytes (total: %d)", len(pkt.Raw), len(clipboardBuffer))
				}
			}

		case protocol.ClipboardDataEnd:
			if receivingClipboard && len(clipboardBuffer) > 0 {
				text := decompressAndParseClipboard(clipboardBuffer)
				if text != "" {
					log.Printf("[recv] Clipboard: %d chars", len(text))
					clip.SetText(text)
				}
				clipboardBuffer = nil
				receivingClipboard = false
			}

		case protocol.Matrix:
			log.Printf("[recv] Matrix update")

		case protocol.MachineSwitched:
			// Windows MWB sends this when mouse hits the edge coming back to us
			log.Printf("[recv] MachineSwitched from %d - returning to local mode", pkt.Header.Src)
			evdev.Ungrab()

		default:
			if debug {
				log.Printf("[recv] Packet %d (unhandled)", pkt.Header.Type)
			}
		}
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

func buttonFlags(code uint16, pressed bool) int32 {
	switch code {
	case input.BTN_LEFT:
		if pressed {
			return input.WM_LBUTTONDOWN
		}
		return input.WM_LBUTTONUP
	case input.BTN_RIGHT:
		if pressed {
			return input.WM_RBUTTONDOWN
		}
		return input.WM_RBUTTONUP
	case input.BTN_MIDDLE:
		if pressed {
			return input.WM_MBUTTONDOWN
		}
		return input.WM_MBUTTONUP
	}
	return 0
}

func clamp(v, min, max int32) int32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func runDemo(cfg *config.Config) {
	fmt.Println("=== DEMO MODE ===")
	fmt.Printf("Screen: %dx%d\n", cfg.ScreenWidth, cfg.ScreenHeight)

	vi, err := input.NewVirtualInput(cfg.ScreenWidth, cfg.ScreenHeight)
	if err != nil {
		fmt.Printf("Failed: %v\n", err)
		return
	}
	defer vi.Close()

	fmt.Println("Testing cursor movement...")
	time.Sleep(1 * time.Second)

	centerX, centerY := int32(32768), int32(32768)
	vi.InjectMouse(centerX, centerY, 0, 0)
	time.Sleep(500 * time.Millisecond)

	offset := int32(15000)
	positions := []struct{ x, y int32 }{
		{centerX - offset, centerY - offset},
		{centerX + offset, centerY - offset},
		{centerX + offset, centerY + offset},
		{centerX - offset, centerY + offset},
		{centerX, centerY},
	}

	for _, pos := range positions {
		vi.InjectMouse(pos.x, pos.y, 0, 0)
		time.Sleep(300 * time.Millisecond)
	}

	fmt.Println("Done! Did the cursor move?")
}

// emergencyKillSwitch monitors for F1 key and kills the app immediately
// This is a safety mechanism to prevent getting locked out
// F1 is KEY_F1 = 59 in Linux input event codes
func emergencyKillSwitch(ctx context.Context, evdev *input.EvdevCapture) {
	// Open keyboard device directly for monitoring
	kbdPath := "/dev/input/event7"
	if _, err := os.Stat(kbdPath); err != nil {
		// Try to find keyboard
		if path, err := input.FindKeyboardDevice(); err == nil {
			kbdPath = path
		}
	}

	f, err := os.Open(kbdPath)
	if err != nil {
		log.Printf("[emergency] Cannot open keyboard for monitoring: %v", err)
		return
	}
	defer f.Close()

	buf := make([]byte, 24)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, err := f.Read(buf)
		if err != nil {
			return
		}

		// Check for F1 key (code 59) press
		// Input event: [time 16 bytes][type 2 bytes][code 2 bytes][value 4 bytes]
		code := uint16(buf[18]) | uint16(buf[19])<<8
		value := int32(buf[20]) | int32(buf[21])<<8 | int32(buf[22])<<16 | int32(buf[23])<<24
		evType := uint16(buf[16]) | uint16(buf[17])<<8
		log.Printf("[emergency] Event type %d, code %d, value %d", evType, code, value)
		if evType == 1 && code == 59 && value == 1 { // EV_KEY, F1, press
			log.Println("[EMERGENCY] F1 key detected - releasing all devices and exiting!")
			evdev.Close()
			os.Exit(1)
		}
	}
}
