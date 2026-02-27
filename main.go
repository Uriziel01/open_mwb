package main

import (
	"fmt"
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

	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Printf("=== open-mwb v%s ===", Version)
	log.Printf("Mode: %s | Edge: %s | Screen: %dx%d | MachineID: %d",
		cfg.Mode, cfg.Edge, cfg.ScreenWidth, cfg.ScreenHeight, cfg.MachineID)

	client, err := connectClient(cfg)
	if err != nil {
		log.Fatalf("Connection failed: %v", err)
	}

	if cfg.Mode == "tui" {
		screen := tui.New(60, 20, cfg.Edge, client, cfg.MachineID, client.RemoteMachineID, cfg.Debug)
		screen.Run()
		client.Conn.Close()
		return
	}

	vi, err := input.NewVirtualInput(cfg.ScreenWidth, cfg.ScreenHeight)
	if err != nil {
		log.Fatalf("Failed to create virtual input: %v", err)
	}
	defer vi.Close()

	evdev := setupInputCapture(cfg, client)
	defer evdev.Close()

	clip := clipboard.New()
	setupClipboard(clip, client, cfg)

	go evdev.RunMouseLoop()
	go evdev.RunKeyboardLoop()
	go clip.Watch()
	go sendHeartbeats(client, cfg)
	go receiveLoop(client, vi, clip, cfg.Debug)

	log.Println("")
	log.Println("Ready! Move your mouse to the screen edge to switch.")
	log.Println("Press ScrollLock to return input to this machine.")
	log.Println("Press Ctrl+C to quit.")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	clip.Stop()
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

	evdev := input.NewEvdevCapture(cfg.ScreenWidth, cfg.ScreenHeight, cfg.Edge)
	if err := evdev.Open(mouseDev, kbdDev); err != nil {
		log.Fatalf("Failed to open input devices: %v", err)
	}

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
			return
		}

		flags := input.WM_KEYDOWN
		if !pressed {
			flags = input.WM_KEYUP
		}

		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Keyboard, sendMu,
			&protocol.KeyboardData{Vk: vk, Flags: int32(flags)})
	}

	return evdev
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
		log.Printf("[clipboard] Sending %d chars", len(content))
		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.ClipboardText, sendMu,
			[]byte(content))
	}
}

func sendPacket(client *network.Client, id, src, dst uint32, pktType protocol.PackageType, mu sync.Mutex, payload interface{}) {
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
		log.Printf("[send] Failed to send %v: %v", pktType, err)
	}
}

func sendHeartbeats(client *network.Client, cfg *config.Config) {
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
	for range ticker.C {
		sendPacket(client, nextID(), cfg.MachineID, client.RemoteMachineID, protocol.Heartbeat, sendMu, nil)
	}
}

func receiveLoop(client *network.Client, vi *input.VirtualInput, clip *clipboard.Clipboard, debug bool) {
	for {
		pkt, err := client.Receive()
		if err != nil {
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
				text := string(pkt.Raw)
				log.Printf("[recv] Clipboard: %d chars", len(text))
				clip.SetText(text)
			}

		case protocol.Matrix, protocol.Heartbeat, protocol.Heartbeat_ex, 
			protocol.Hi, protocol.HideMouse, protocol.MachineSwitched, 
			protocol.HandshakeAck:
			// Silently ignore these common packets
	}
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
}
