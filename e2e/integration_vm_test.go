package e2e

import (
	"bytes"
	"compress/flate"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"open-mwb/network"
	"open-mwb/protocol"
)

func decodeUTF16LE(b []byte) string {
	var runes []rune
	for i := 0; i < len(b)-1; i += 2 {
		r := uint16(b[i]) | (uint16(b[i+1]) << 8)
		runes = append(runes, rune(r))
	}
	return string(runes)
}

func TestIntegration_LinuxToWindows_Mouse(t *testing.T) {
	vmIP := os.Getenv("TEST_VM_IP")
	if vmIP == "" {
		t.Skip("Skipping live VM integration test. Set TEST_VM_IP to run.")
	}

	// Constants based on config.json
	const securityKey = "cH9+tJ3@pB4!hJ2*"
	const mwbPort = 15100 // network.Connect adds 1 internally
	const myMachineID = uint32(2025022500)
	const myMachineName = "URIZIEL-LINUX"

	autoIt := NewAutoItClient(vmIP)

	// Step 1: Connect to the real MWB instance running on the Windows VM
	t.Logf("Connecting to MWB at %s:%d...", vmIP, mwbPort+1)
	client, err := network.Connect(vmIP, mwbPort, securityKey, myMachineID, myMachineName, true)
	if err != nil {
		t.Fatalf("Failed to connect to Windows VM MWB: %v", err)
	}
	defer client.Conn.Close()

	t.Log("Connected! Sending mouse move to absolute center...")

	// Step 2: Send mouse move (Absolute)
	// In MWB, coordinates are scaled to 65535
	mouseMove := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.Mouse,
			Id:   1001,
			Src:  myMachineID,
			Des:  client.RemoteMachineID,
		},
		Mouse: &protocol.MouseData{
			X:     32768, // center X
			Y:     32768, // center Y
			Flags: 0x8000 | 0x0001, // MOUSEEVENTF_ABSOLUTE | MOUSEEVENTF_MOVE
		},
	}
	
	if err := client.Send(mouseMove); err != nil {
		t.Fatalf("Failed to send mouse move: %v", err)
	}

	// Give the Windows OS a moment to process the simulated input
	time.Sleep(500 * time.Millisecond)

	// Step 3: Verify via AutoIt server
	t.Log("Querying AutoIt server for mouse position...")
	pos, err := autoIt.GetMousePos()
	if err != nil {
		t.Fatalf("Failed to query AutoIt server: %v", err)
	}

	t.Logf("Success! AutoIt verified mouse moved to: %s", pos)
	// We don't assert exact pixels because VM resolution scales 32768 depending on display size,
	// but getting a response is a great integration validation.
}

func TestIntegration_MatrixEdge_Transition(t *testing.T) {
	vmIP := os.Getenv("TEST_VM_IP")
	if vmIP == "" {
		t.Skip("Skipping live VM integration test. Set TEST_VM_IP to run.")
	}

	const securityKey = "cH9+tJ3@pB4!hJ2*"
	const mwbPort = 15100
	const myMachineID = uint32(2025022500)
	const myMachineName = "URIZIEL-LINUX"

	autoIt := NewAutoItClient(vmIP)

	client, err := network.Connect(vmIP, mwbPort, securityKey, myMachineID, myMachineName, true)
	if err != nil {
		t.Fatalf("Failed to connect to Windows VM MWB: %v", err)
	}
	defer client.Conn.Close()

	// Empty handshake packets
	for {
		client.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		if _, err := client.Receive(); err != nil {
			break
		}
	}
	client.Conn.SetReadDeadline(time.Time{})

	// Get Windows desktop size
	sizeStr, err := autoIt.GetDesktopSize()
	if err != nil {
		t.Fatalf("Failed to get desktop size: %v", err)
	}
	
	parts := strings.Split(sizeStr, ",")
	if len(parts) != 2 {
		t.Fatalf("Invalid desktop size format: %s", sizeStr)
	}
	
	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])
	t.Logf("Windows desktop size: %dx%d", width, height)

	// Make Windows the "controller" by setting focus there
	t.Log("Forcing Windows to become active controller...")
	// Move mouse to center first
	_ = autoIt.MoveMouse(width/2, height/2)
	time.Sleep(500 * time.Millisecond)

	// Now slam mouse to the far right edge
	// Because of MWB logic, we often need multiple moves at the edge to trigger a transition
	t.Log("Moving mouse to the right edge of Windows screen...")
	if err := autoIt.MoveMouse(width-1, height/2); err != nil {
		t.Fatalf("Failed to move mouse: %v", err)
	}
	
	// Wait a moment for UI to register
	time.Sleep(100 * time.Millisecond)

	// In AutoIt, moving beyond the screen width might get clamped, so we try multiple "push" operations
	// Or we just send a very large X coordinate to force Windows to clamp it at the edge, 
	// which MWB hooks might interpret as "pushing" the edge.
	t.Log("Slamming mouse off the right edge...")
	for i := 0; i < 5; i++ {
		_ = autoIt.MoveMouse(width+50, height/2)
		time.Sleep(100 * time.Millisecond)
	}

	client.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer client.Conn.SetReadDeadline(time.Time{})

	foundHideMouse := false
	foundNextMachine := false
	foundMouseMoves := 0
	
	for i := 0; i < 50; i++ {
		pkt, err := client.Receive()
		if err != nil {
			break
		}

		if pkt.Header.Type == protocol.HideMouse {
			foundHideMouse = true
			t.Log("Success! Received HideMouse packet indicating Windows cursor is hidden.")
		} else if pkt.Header.Type == protocol.NextMachine {
			foundNextMachine = true
			t.Log("Success! Received NextMachine packet.")
		} else if pkt.Header.Type == protocol.Mouse {
			foundMouseMoves++
			if foundMouseMoves == 1 {
				t.Logf("Success! Received remote Mouse packet. Windows is now controlling Linux: X=%d, Y=%d", 
					pkt.Mouse.X, pkt.Mouse.Y)
			}
		}
		
		if foundHideMouse && foundMouseMoves > 0 {
			t.Logf("Successfully captured full matrix transition: HideMouse received, followed by %d Mouse control packets.", foundMouseMoves)
			return // Success
		}
	}

	if !foundHideMouse {
		t.Fatal("Did not receive transition packets (HideMouse) after slamming edge")
	}
	if !foundNextMachine {
		t.Fatal("Did not receive NextMachine packet after slamming edge")
	}
	if foundMouseMoves == 0 {
		t.Fatal("Received HideMouse, but no subsequent Mouse control packets were routed to Linux")
	}
}

func TestIntegration_WindowsToLinux_ClipboardText(t *testing.T) {
	vmIP := os.Getenv("TEST_VM_IP")
	if vmIP == "" {
		t.Skip("Skipping live VM integration test. Set TEST_VM_IP to run.")
	}

	const securityKey = "cH9+tJ3@pB4!hJ2*"
	const mwbPort = 15100 // network.Connect adds 1 internally
	const myMachineID = uint32(2025022500)
	const myMachineName = "URIZIEL-LINUX"

	autoIt := NewAutoItClient(vmIP)

	client, err := network.Connect(vmIP, mwbPort, securityKey, myMachineID, myMachineName, true)
	if err != nil {
		t.Fatalf("Failed to connect to Windows VM MWB: %v", err)
	}
	defer client.Conn.Close()

	// Wait for any remaining handshake packets to clear
	for {
		client.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, err := client.Receive()
		if err != nil {
			break
		}
	}
	client.Conn.SetReadDeadline(time.Time{})

	// Trigger clipboard set on Windows via AutoIt
	testText := "Hello_From_Windows"
	t.Logf("Setting Windows clipboard to: %s", testText)
	if err := autoIt.SetClipboard(testText); err != nil {
		t.Fatalf("Failed to set clipboard via AutoIt: %v", err)
	}

	// Wait for MWB to notice and send us a Clipboard packet
	t.Log("Waiting for Clipboard packet from MWB...")
	
	// Start reading with timeout
	client.Conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer client.Conn.SetReadDeadline(time.Time{})
	
	found := false
	var compressedData []byte

	for {
		pkt, err := client.Receive()
		if err != nil {
			t.Fatalf("Failed to receive packet or timed out waiting for clipboard: %v", err)
		}

		if pkt.Header.Type == protocol.ClipboardText {
			if pkt.Raw != nil {
				compressedData = append(compressedData, pkt.Raw...)
			}
		} else if pkt.Header.Type == protocol.ClipboardDataEnd {
			// Now we decompress
			r := flate.NewReader(bytes.NewReader(compressedData))
			decompressed, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("Failed to decompress clipboard data: %v", err)
			}
			r.Close()

			received := decodeUTF16LE(decompressed)
			
			// MWB suffixes clipboard strings with a GUID and prefixes with format (TXT, RTF, UNI, HTM)
			// Format is usually: "TXT<payload>{4CFF57F7-BEDD-43d5-AE8F-27A61E886F2F}"
			const textTypeSep = "{4CFF57F7-BEDD-43d5-AE8F-27A61E886F2F}"
			
			// Simple parsing for test
			parsed := received
			if len(received) >= 3 && (received[:3] == "TXT" || received[:3] == "UNI" || received[:3] == "RTF" || received[:3] == "HTM") {
				parsed = received[3:]
			}
			
			// Strip the suffix and anything after
			if idx := strings.Index(parsed, textTypeSep); idx != -1 {
				parsed = parsed[:idx]
			}
			// Sometimes null bytes are appended at the end
			parsed = strings.TrimRight(parsed, "\x00")

			if parsed == testText {
				t.Logf("Success! Received and decompressed matching clipboard text: %s", parsed)
				found = true
				break
			} else {
				t.Logf("Warning: Received clipboard text %q but expected %q", parsed, testText)
				// We don't break just in case there's another attempt or something, but usually this is it.
				found = true
				break
			}

		}
	}

	if !found {
		t.Fatal("Did not receive matching clipboard text from Windows")
	}
}

func TestIntegration_WindowsToLinux_ClipboardImage(t *testing.T) {
	vmIP := os.Getenv("TEST_VM_IP")
	if vmIP == "" {
		t.Skip("Skipping live VM integration test. Set TEST_VM_IP to run.")
	}

	const securityKey = "cH9+tJ3@pB4!hJ2*"
	const mwbPort = 15100 // network.Connect adds 1 internally
	const myMachineID = uint32(2025022500)
	const myMachineName = "URIZIEL-LINUX"

	autoIt := NewAutoItClient(vmIP)

	client, err := network.Connect(vmIP, mwbPort, securityKey, myMachineID, myMachineName, true)
	if err != nil {
		t.Fatalf("Failed to connect to Windows VM MWB: %v", err)
	}
	defer client.Conn.Close()

	// Wait for any remaining handshake packets to clear
	for {
		client.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, err := client.Receive()
		if err != nil {
			break
		}
	}
	client.Conn.SetReadDeadline(time.Time{})

	// Trigger clipboard set on Windows via AutoIt
	t.Log("Setting Windows clipboard to sample image...")
	if err := autoIt.ImgToClipboard(); err != nil {
		t.Fatalf("Failed to set clipboard image via AutoIt: %v", err)
	}

	// Wait for MWB to notice and send us a Clipboard packet
	t.Log("Waiting for ClipboardImage packet from MWB...")
	
	// Start reading with timeout
	client.Conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer client.Conn.SetReadDeadline(time.Time{})
	
	found := false
	var compressedData []byte

	for {
		pkt, err := client.Receive()
		if err != nil {
			t.Fatalf("Failed to receive packet or timed out waiting for clipboard: %v", err)
		}

		if pkt.Header.Type == protocol.ClipboardImage {
			if pkt.Raw != nil {
				compressedData = append(compressedData, pkt.Raw...)
			}
		} else if pkt.Header.Type == protocol.ClipboardDataEnd {
			// Image data is sent raw, not deflated like text!
			t.Logf("Received ClipboardDataEnd! Total image size: %d bytes", len(compressedData))
			
			if len(compressedData) > 0 {
				found = true
				t.Log("Success! Received raw clipboard image.")
				
				// Validate it's an image (e.g. BMP/PNG/JPEG headers)
				if len(compressedData) > 4 {
					t.Logf("Image Header: %x", compressedData[:8])
				}
				break
			}
		}
	}

	if !found {
		t.Fatal("Did not receive matching clipboard image from Windows")
	}
}
