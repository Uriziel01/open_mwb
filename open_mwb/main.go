package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"mwb-linux/clipboard"
	"mwb-linux/config"
	"mwb-linux/input"
	"mwb-linux/network"
	"mwb-linux/protocol"
	"mwb-linux/tui"
)

func main() {
	cfg := config.Parse()

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
	log.Println("=== Mouse Without Borders - Linux POC ===")
	log.Printf("Mode: %s | Edge: %s | Screen: %dx%d | MachineID: %d",
		cfg.Mode, cfg.Edge, cfg.ScreenWidth, cfg.ScreenHeight, cfg.MachineID)

	// ---- Connect to or listen for Windows MWB ----
	var client *network.Client
	var err error

	// MWB requires reciprocal connections to establish full trust (green status).
	// We MUST start the server concurrently so Windows can connect back.
	if cfg.Mode == "client" || cfg.Mode == "tui" {
		log.Printf("Starting background Server on port %d to accept reciprocal connections...", cfg.ListenPort+1)
		server, err := network.NewServer(cfg.ListenPort, cfg.SecurityKey, cfg.MachineID, cfg.MachineName, cfg.Debug)
		if err != nil {
			log.Printf("Warning: Background server failed to start: %v", err)
		} else {
			go func() {
				defer server.Close()
				for {
					windowsClient, err := server.Accept()
					if err != nil {
						log.Printf("Background server accept error: %v", err)
						return
					}
					log.Printf("Accepted reciprocal connection from Windows MWB (%s)", windowsClient.MachineName)
					
					// Keep the reciprocal connection alive and read from it
					go func(c *network.Client) {
						defer c.Conn.Close()
						for {
							_, err := c.Receive()
							if err != nil {
								log.Printf("Reciprocal connection closed: %v", err)
								return
							}
						}
					}(windowsClient)
				}
			}()
		}
	}

	switch cfg.Mode {
	case "client", "tui":
		log.Printf("Connecting to Windows MWB at %s:%d...", cfg.RemoteAddress, cfg.ListenPort+1)
		client, err = network.Connect(cfg.RemoteAddress, cfg.ListenPort, cfg.SecurityKey, cfg.MachineID, cfg.MachineName, cfg.Debug)
		if err != nil {
			log.Fatalf("Connection failed: %v", err)
		}
		log.Println("Connected and handshake complete!")
	case "server":
		log.Printf("Starting server on port %d, waiting for Windows MWB...", cfg.ListenPort+1)
		server, err := network.NewServer(cfg.ListenPort, cfg.SecurityKey, cfg.MachineID, cfg.MachineName, cfg.Debug)
		if err != nil {
			log.Fatalf("Server start failed: %v", err)
		}
		defer server.Close()

		client, err = server.Accept()
		if err != nil {
			log.Fatalf("Accept failed: %v", err)
		}
		log.Println("Windows MWB connected and handshake complete!")
	default:
		log.Fatalf("Unknown mode: %s (use client, server, or tui)", cfg.Mode)
	}

	// ---- TUI mode: launch debug screen and return ----
	if cfg.Mode == "tui" {
		screen := tui.New(60, 20, cfg.Edge, client, cfg.MachineID, cfg.RemoteMachineID, cfg.Debug)
		screen.Run()
		client.Conn.Close()
		return
	}

	// ---- Setup virtual input devices (for injecting remote input) ----
	vInput, err := input.NewVirtualInput(cfg.ScreenWidth, cfg.ScreenHeight)
	if err != nil {
		log.Fatalf("Failed to create virtual input devices: %v", err)
	}
	defer vInput.Close()

	// ---- Setup evdev capture (for capturing local input) ----
	mouseDev := cfg.MouseDevice
	if mouseDev == "" {
		mouseDev, err = input.FindMouseDevice()
		if err != nil {
			log.Fatalf("Failed to find mouse device: %v (use --mouse to specify manually, or --list-devices to see available)", err)
		}
	}

	kbdDev := cfg.KeyboardDevice
	if kbdDev == "" {
		kbdDev, err = input.FindKeyboardDevice()
		if err != nil {
			log.Fatalf("Failed to find keyboard device: %v (use --keyboard to specify manually, or --list-devices to see available)", err)
		}
	}

	log.Printf("Using mouse: %s", mouseDev)
	log.Printf("Using keyboard: %s", kbdDev)

	evdev := input.NewEvdevCapture(cfg.ScreenWidth, cfg.ScreenHeight, cfg.Edge)
	if err := evdev.Open(mouseDev, kbdDev); err != nil {
		log.Fatalf("Failed to open input devices: %v", err)
	}
	defer evdev.Close()

	// ---- Setup clipboard ----
	clip := clipboard.New()

	// ---- Wire up callbacks ----
	var sendMu sync.Mutex
	packetID := uint32(100)

	nextID := func() uint32 {
		sendMu.Lock()
		defer sendMu.Unlock()
		packetID++
		return packetID
	}

	// When mouse hits edge -> we're now forwarding to remote
	evdev.OnEdgeHit = func() {
		log.Println("[main] Edge hit - now forwarding input to Windows")
	}

	// When ScrollLock is pressed -> return to local
	evdev.OnReturn = func() {
		log.Println("[main] Returning to local control")
	}

	// Forward mouse movements to Windows
	evdev.OnMouseEvent = func(dx, dy, wheelDelta int) {
		flags := int32(input.WinMouseEventFMove)
		if wheelDelta != 0 {
			flags |= input.WinMouseEventFWheel
		}

		pkt := &protocol.GenericData{
			Header: protocol.Header{
				Type: protocol.Mouse,
				Id:   nextID(),
				Src:  cfg.MachineID,
				Des:  cfg.RemoteMachineID,
			},
			Mouse: &protocol.MouseData{
				X:          int32(dx),
				Y:          int32(dy),
				WheelDelta: int32(wheelDelta),
				Flags:      flags,
			},
		}

		sendMu.Lock()
		err := client.Send(pkt)
		sendMu.Unlock()
		if err != nil {
			log.Printf("[main] Failed to send mouse event: %v", err)
		}
	}

	// Forward mouse button events to Windows
	evdev.OnButtonEvent = func(code uint16, pressed bool) {
		flags := int32(0)
		switch code {
		case input.BTN_LEFT:
			if pressed {
				flags = input.WinMouseEventFLeftDown
			} else {
				flags = input.WinMouseEventFLeftUp
			}
		case input.BTN_RIGHT:
			if pressed {
				flags = input.WinMouseEventFRightDown
			} else {
				flags = input.WinMouseEventFRightUp
			}
		case input.BTN_MIDDLE:
			if pressed {
				flags = input.WinMouseEventFMiddleDown
			} else {
				flags = input.WinMouseEventFMiddleUp
			}
		}

		pkt := &protocol.GenericData{
			Header: protocol.Header{
				Type: protocol.Mouse,
				Id:   nextID(),
				Src:  cfg.MachineID,
				Des:  cfg.RemoteMachineID,
			},
			Mouse: &protocol.MouseData{
				Flags: flags,
			},
		}

		sendMu.Lock()
		err := client.Send(pkt)
		sendMu.Unlock()
		if err != nil {
			log.Printf("[main] Failed to send button event: %v", err)
		}
	}

	// Forward keyboard events to Windows
	evdev.OnKeyEvent = func(code uint16, pressed bool) {
		vk, ok := input.LinuxToVK[code]
		if !ok {
			return
		}

		flags := int32(0)
		if !pressed {
			flags = input.WinKeyEventFKeyUp
		}

		pkt := &protocol.GenericData{
			Header: protocol.Header{
				Type:     protocol.Keyboard,
				Id:       nextID(),
				Src:      cfg.MachineID,
				Des:      cfg.RemoteMachineID,
				DateTime: uint64(time.Now().UnixNano() / 10000),
			},
			Keyboard: &protocol.KeyboardData{
				Vk:    vk,
				Flags: flags,
			},
		}

		sendMu.Lock()
		err := client.Send(pkt)
		sendMu.Unlock()
		if err != nil {
			log.Printf("[main] Failed to send keyboard event: %v", err)
		}
	}

	// Clipboard: when local clipboard changes, send to Windows
	clip.OnChange = func(content string) {
		log.Printf("[clipboard] Local clipboard changed (%d chars), sending to Windows", len(content))

		// For text clipboard, we send ClipboardText with the text in Raw
		textBytes := []byte(content)

		pkt := &protocol.GenericData{
			Header: protocol.Header{
				Type:     protocol.ClipboardText,
				Id:       nextID(),
				Src:      cfg.MachineID,
				Des:      cfg.RemoteMachineID,
				DateTime: uint64(time.Now().UnixNano() / 10000),
			},
			Raw: textBytes,
		}

		sendMu.Lock()
		err := client.Send(pkt)
		sendMu.Unlock()
		if err != nil {
			log.Printf("[clipboard] Failed to send clipboard: %v", err)
		}
	}

	// ---- Start goroutines ----
	go evdev.RunMouseLoop()
	go evdev.RunKeyboardLoop()
	go clip.Watch()

	// Heartbeat sender
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			pkt := &protocol.GenericData{
				Header: protocol.Header{
					Type:     protocol.Heartbeat,
					Id:       nextID(),
					Src:      cfg.MachineID,
					Des:      cfg.RemoteMachineID,
					DateTime: uint64(time.Now().UnixNano() / 10000),
				},
			}
			sendMu.Lock()
			client.Send(pkt)
			sendMu.Unlock()
		}
	}()

	// ---- Main receive loop (incoming from Windows) ----
	go func() {
		for {
			pkt, err := client.Receive()
			if err != nil {
				log.Printf("[recv] Error: %v", err)
				continue
			}

			switch pkt.Header.Type {
			case protocol.Mouse:
				if pkt.Mouse != nil {
					vInput.InjectMouse(pkt.Mouse.X, pkt.Mouse.Y, pkt.Mouse.WheelDelta, pkt.Mouse.Flags)
				}

			case protocol.Keyboard:
				if pkt.Keyboard != nil {
					vInput.InjectKeyboard(pkt.Keyboard.Vk, pkt.Keyboard.Flags)
				}

			case protocol.ClipboardText:
				if pkt.Raw != nil {
					text := string(pkt.Raw)
					log.Printf("[recv] Clipboard text from Windows (%d chars)", len(text))
					clip.SetText(text)
				}

			case protocol.Heartbeat:
				// Silently acknowledge

			case protocol.Matrix:
				log.Printf("[recv] Matrix topology update from Windows")

			default:
				log.Printf("[recv] Packet type %d (unhandled)", pkt.Header.Type)
			}
		}
	}()

	log.Println("")
	log.Println("Ready! Move your mouse to the screen edge to switch.")
	log.Println("Press ScrollLock to return input to this machine.")
	log.Println("Press Ctrl+C to quit.")

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	clip.Stop()
	evdev.Close()
	vInput.Close()
	client.Conn.Close()

	// Workaround: flag.Parse is called in config.Parse, suppress unused import
	_ = flag.CommandLine
}
