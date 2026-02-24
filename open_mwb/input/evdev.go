package input

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Linux input event structure (struct input_event from linux/input.h)
// On 64-bit: timeval is 16 bytes (tv_sec 8 + tv_usec 8), type 2, code 2, value 4 = 24 bytes
type InputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

const inputEventSize = 24

// Event types
const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_REL = 0x02
	EV_ABS = 0x03
)

// Relative axes
const (
	REL_X     = 0x00
	REL_Y     = 0x01
	REL_WHEEL = 0x08
)

// Button codes
const (
	BTN_LEFT   = 0x110
	BTN_RIGHT  = 0x111
	BTN_MIDDLE = 0x112
)

// EVIOCGRAB ioctl
const EVIOCGRAB = 0x40044590

// EVIOCGNAME ioctl base
func eviocgname(length int) uintptr {
	// _IOC(_IOC_READ, 'E', 0x06, len)
	return uintptr(2<<30 | uintptr(length)<<16 | 'E'<<8 | 0x06)
}

// EvdevCapture reads raw input events from /dev/input/event* devices.
// It tracks the virtual cursor position and detects screen edge transitions.
type EvdevCapture struct {
	mu            sync.Mutex
	mouseFile     *os.File
	kbdFile       *os.File
	mouseGrabbed  bool
	kbdGrabbed    bool

	// Virtual cursor position tracking
	cursorX int
	cursorY int

	// Screen bounds (set from config)
	screenW int
	screenH int

	// Edge to trigger on
	// "right" = when cursor hits right edge, switch to remote
	// "left"  = when cursor hits left edge, switch to remote
	Edge string

	// Callback when edge is triggered (entering remote mode)
	OnEdgeHit func()

	// Callback when escape key combo is pressed to return (leaving remote mode)
	OnReturn func()

	// Callback for forwarding events while in remote mode
	OnMouseEvent    func(dx, dy, wheelDelta int)
	OnKeyEvent      func(code uint16, pressed bool)
	OnButtonEvent   func(code uint16, pressed bool)

	// State: are we currently controlling the remote machine?
	IsRemote bool
}

// NewEvdevCapture creates a new evdev input capture.
func NewEvdevCapture(screenW, screenH int, edge string) *EvdevCapture {
	return &EvdevCapture{
		screenW: screenW,
		screenH: screenH,
		cursorX: screenW / 2,
		cursorY: screenH / 2,
		Edge:    edge,
	}
}

// findDevice scans /dev/input/event* for a device whose name contains the keyword.
func findDevice(keyword string) (string, error) {
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return "", err
	}

	for _, path := range matches {
		f, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			continue
		}

		nameBuf := make([]byte, 256)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), eviocgname(len(nameBuf)), uintptr(unsafe.Pointer(&nameBuf[0])))
		f.Close()

		if errno != 0 {
			continue
		}

		name := strings.ToLower(strings.TrimRight(string(nameBuf), "\x00"))
		if strings.Contains(name, strings.ToLower(keyword)) {
			return path, nil
		}
	}

	return "", fmt.Errorf("no input device found matching %q", keyword)
}

// FindMouseDevice finds the first mouse/pointer device.
func FindMouseDevice() (string, error) {
	// Try common mouse device name patterns
	for _, kw := range []string{"mouse", "pointer", "touchpad", "trackpad"} {
		path, err := findDevice(kw)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no mouse device found in /dev/input/")
}

// FindKeyboardDevice finds the first keyboard device.
func FindKeyboardDevice() (string, error) {
	for _, kw := range []string{"keyboard", "kbd"} {
		path, err := findDevice(kw)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no keyboard device found in /dev/input/")
}

// ListDevices prints all /dev/input/event* device names for debugging.
func ListDevices() {
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		f, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			continue
		}

		nameBuf := make([]byte, 256)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), eviocgname(len(nameBuf)), uintptr(unsafe.Pointer(&nameBuf[0])))
		f.Close()

		if errno != 0 {
			continue
		}
		name := strings.TrimRight(string(nameBuf), "\x00")
		fmt.Printf("  %s: %s\n", path, name)
	}
}

// Open opens the mouse and keyboard device files.
func (e *EvdevCapture) Open(mousePath, kbdPath string) error {
	var err error

	e.mouseFile, err = os.OpenFile(mousePath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open mouse device %s: %w", mousePath, err)
	}

	e.kbdFile, err = os.OpenFile(kbdPath, os.O_RDONLY, 0)
	if err != nil {
		e.mouseFile.Close()
		return fmt.Errorf("failed to open keyboard device %s: %w", kbdPath, err)
	}

	return nil
}

// Grab exclusively grabs both mouse and keyboard so Wayland stops seeing them.
func (e *EvdevCapture) Grab() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.mouseGrabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, e.mouseFile.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			return fmt.Errorf("failed to grab mouse: %v", errno)
		}
		e.mouseGrabbed = true
	}

	if !e.kbdGrabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, e.kbdFile.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			return fmt.Errorf("failed to grab keyboard: %v", errno)
		}
		e.kbdGrabbed = true
	}

	e.IsRemote = true
	log.Println("[evdev] Grabbed input devices - remote mode active")
	return nil
}

// Ungrab releases the exclusive grab, returning input to Wayland.
func (e *EvdevCapture) Ungrab() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.mouseGrabbed {
		unix.Syscall(unix.SYS_IOCTL, e.mouseFile.Fd(), EVIOCGRAB, 0)
		e.mouseGrabbed = false
	}

	if e.kbdGrabbed {
		unix.Syscall(unix.SYS_IOCTL, e.kbdFile.Fd(), EVIOCGRAB, 0)
		e.kbdGrabbed = false
	}

	e.IsRemote = false
	// Reset cursor to center so next edge detection works cleanly
	e.cursorX = e.screenW / 2
	e.cursorY = e.screenH / 2
	log.Println("[evdev] Released input devices - local mode active")
	return nil
}

// RunMouseLoop reads mouse events in a blocking loop. Call in a goroutine.
func (e *EvdevCapture) RunMouseLoop() {
	buf := make([]byte, inputEventSize)
	for {
		_, err := e.mouseFile.Read(buf)
		if err != nil {
			log.Printf("[evdev] Mouse read error: %v", err)
			return
		}

		ev := parseEvent(buf)

		if e.IsRemote {
			// Forward events to the remote machine
			e.handleRemoteMouseEvent(ev)
		} else {
			// Track virtual cursor for edge detection
			e.handleLocalMouseEvent(ev)
		}
	}
}

// RunKeyboardLoop reads keyboard events in a blocking loop. Call in a goroutine.
func (e *EvdevCapture) RunKeyboardLoop() {
	buf := make([]byte, inputEventSize)
	for {
		_, err := e.kbdFile.Read(buf)
		if err != nil {
			log.Printf("[evdev] Keyboard read error: %v", err)
			return
		}

		ev := parseEvent(buf)

		if ev.Type != EV_KEY {
			continue
		}

		if e.IsRemote {
			// Check for escape combo: Ctrl+Alt+Escape to return to local
			// For now just use Scroll Lock (KEY_SCROLLLOCK = 70) as toggle
			if ev.Code == 70 && ev.Value == 1 { // ScrollLock press
				log.Println("[evdev] ScrollLock detected - returning to local")
				e.Ungrab()
				if e.OnReturn != nil {
					e.OnReturn()
				}
				continue
			}

			// Forward keyboard event
			if e.OnKeyEvent != nil {
				pressed := ev.Value == 1 || ev.Value == 2 // 1=press, 2=repeat
				e.OnKeyEvent(ev.Code, pressed)
			}
		}
	}
}

func (e *EvdevCapture) handleLocalMouseEvent(ev InputEvent) {
	if ev.Type == EV_REL {
		switch ev.Code {
		case REL_X:
			e.cursorX += int(ev.Value)
			if e.cursorX < 0 {
				e.cursorX = 0
			}
			if e.cursorX >= e.screenW {
				e.cursorX = e.screenW - 1
			}
		case REL_Y:
			e.cursorY += int(ev.Value)
			if e.cursorY < 0 {
				e.cursorY = 0
			}
			if e.cursorY >= e.screenH {
				e.cursorY = e.screenH - 1
			}
		}

		// Check edge
		edgeHit := false
		switch e.Edge {
		case "right":
			edgeHit = e.cursorX >= e.screenW-1
		case "left":
			edgeHit = e.cursorX <= 0
		case "top":
			edgeHit = e.cursorY <= 0
		case "bottom":
			edgeHit = e.cursorY >= e.screenH-1
		}

		if edgeHit {
			log.Printf("[evdev] Edge %q hit at cursor (%d, %d) - switching to remote", e.Edge, e.cursorX, e.cursorY)
			e.Grab()
			if e.OnEdgeHit != nil {
				e.OnEdgeHit()
			}
		}
	}
}

func (e *EvdevCapture) handleRemoteMouseEvent(ev InputEvent) {
	switch ev.Type {
	case EV_REL:
		dx, dy, wheel := 0, 0, 0
		switch ev.Code {
		case REL_X:
			dx = int(ev.Value)
		case REL_Y:
			dy = int(ev.Value)
		case REL_WHEEL:
			wheel = int(ev.Value) * 120 // Windows expects multiples of 120
		}
		if e.OnMouseEvent != nil && (dx != 0 || dy != 0 || wheel != 0) {
			e.OnMouseEvent(dx, dy, wheel)
		}

	case EV_KEY:
		// Mouse button events
		if ev.Code >= BTN_LEFT && ev.Code <= BTN_MIDDLE {
			if e.OnButtonEvent != nil {
				pressed := ev.Value == 1
				e.OnButtonEvent(ev.Code, pressed)
			}
		}
	}
}

func parseEvent(buf []byte) InputEvent {
	return InputEvent{
		TimeSec:  int64(binary.LittleEndian.Uint64(buf[0:8])),
		TimeUsec: int64(binary.LittleEndian.Uint64(buf[8:16])),
		Type:     binary.LittleEndian.Uint16(buf[16:18]),
		Code:     binary.LittleEndian.Uint16(buf[18:20]),
		Value:    int32(binary.LittleEndian.Uint32(buf[20:24])),
	}
}

// Close releases grabs and closes device files.
func (e *EvdevCapture) Close() {
	e.Ungrab()
	if e.mouseFile != nil {
		e.mouseFile.Close()
	}
	if e.kbdFile != nil {
		e.kbdFile.Close()
	}
}
