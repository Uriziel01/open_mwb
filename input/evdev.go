//go:build linux
// +build linux

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

type InputEvent struct {
	TimeSec  int64
	TimeUsec int64
	Type     uint16
	Code     uint16
	Value    int32
}

const inputEventSize = 24

const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_REL = 0x02
	EV_ABS = 0x03
	REL_X  = 0x00
	REL_Y  = 0x01
	REL_WHEEL = 0x08
)

const EVIOCGRAB = 0x40044590

func eviocgname(length int) uintptr {
	return uintptr(2<<30 | uintptr(length)<<16 | 'E'<<8 | 0x06)
}

type EvdevCapture struct {
	mu            sync.Mutex
	mouseFile     *os.File
	kbdFile       *os.File
	mouseGrabbed  bool
	kbdGrabbed    bool
	cursorX       int
	cursorY       int
	screenW       int
	screenH       int
	Edge          string
	IsRemote      bool
	OnEdgeHit     func()
	OnReturn      func()
	OnMouseEvent  func(dx, dy, wheelDelta int)
	OnKeyEvent    func(code uint16, pressed bool)
	OnButtonEvent func(code uint16, pressed bool)
	pressedKeys   map[uint16]bool
}

func NewEvdevCapture(screenW, screenH int, edge string) *EvdevCapture {
	return &EvdevCapture{
		screenW:     screenW,
		screenH:     screenH,
		cursorX:     screenW / 2,
		cursorY:     screenH / 2,
		Edge:        edge,
		pressedKeys: make(map[uint16]bool),
	}
}

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

	return "", fmt.Errorf("no device found matching %q", keyword)
}

func FindMouseDevice() (string, error) {
	for _, kw := range []string{"logitech"} {
		path, err := findDevice(kw)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no mouse found")
}

func FindKeyboardDevice() (string, error) {
	return "/dev/input/event4", nil
}

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

func (e *EvdevCapture) Open(mousePath, kbdPath string) error {
	var err error

	e.mouseFile, err = os.OpenFile(mousePath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open mouse %s: %w", mousePath, err)
	}

	e.kbdFile, err = os.OpenFile(kbdPath, os.O_RDONLY, 0)
	if err != nil {
		e.mouseFile.Close()
		return fmt.Errorf("open keyboard %s: %w", kbdPath, err)
	}

	return nil
}

func (e *EvdevCapture) Grab() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.mouseGrabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, e.mouseFile.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			return fmt.Errorf("grab mouse: %v", errno)
		}
		e.mouseGrabbed = true
	}

	if !e.kbdGrabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, e.kbdFile.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			return fmt.Errorf("grab keyboard: %v", errno)
		}
		e.kbdGrabbed = true
	}

	e.IsRemote = true

	// Release all pressed keys AFTER grabbing and setting remote mode
	// This ensures KEY UP events are sent to remote, not processed locally
	for code := range e.pressedKeys {
		if e.OnKeyEvent != nil {
			e.OnKeyEvent(code, false)
		}
		delete(e.pressedKeys, code)
	}

	log.Println("[evdev] Grabbed devices - remote mode")
	return nil
}

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
	e.cursorX = e.screenW / 2
	e.cursorY = e.screenH / 2
	log.Println("[evdev] Released devices - local mode")
	return nil
}

func (e *EvdevCapture) RunMouseLoop() {
	buf := make([]byte, inputEventSize)
	for {
		_, err := e.mouseFile.Read(buf)
		if err != nil {
			return
		}

		ev := parseEvent(buf)

		if e.IsRemote {
			e.handleRemoteMouseEvent(ev)
		} else {
			e.handleLocalMouseEvent(ev)
		}
	}
}

func (e *EvdevCapture) RunKeyboardLoop() {
	buf := make([]byte, inputEventSize)
	for {
		_, err := e.kbdFile.Read(buf)
		if err != nil {
			return
		}

		ev := parseEvent(buf)

		if ev.Type != EV_KEY {
			continue
		}

		pressed := ev.Value == 1 || ev.Value == 2

		if e.IsRemote {
			// ScrollLock to return to local mode
			if ev.Code == 70 && ev.Value == 1 {
				log.Println("[evdev] ScrollLock - returning to local")
				e.Ungrab()
				if e.OnReturn != nil {
					e.OnReturn()
				}
				continue
			}

			if e.OnKeyEvent != nil {
				e.OnKeyEvent(ev.Code, pressed)
			}
		} else {
			// Track pressed keys in local mode
			e.mu.Lock()
			if pressed {
				e.pressedKeys[ev.Code] = true
			} else {
				delete(e.pressedKeys, ev.Code)
			}
			e.mu.Unlock()

			// Right Shift + Right Ctrl to enter remote mode (alternative to edge detection)
			if ev.Code == 54 && ev.Value == 1 { // Right Shift
				if _, ok := e.pressedKeys[97]; ok { // Right Ctrl (97) is also pressed
					log.Println("[evdev] Right Shift+Ctrl - entering remote mode")
					e.Grab()
					if e.OnEdgeHit != nil {
						e.OnEdgeHit()
					}
				}
			}
		}
	}
}

func (e *EvdevCapture) handleLocalMouseEvent(ev InputEvent) {
	if ev.Type == EV_REL {
		switch ev.Code {
		case REL_X:
			e.cursorX += int(ev.Value)
			e.cursorX = clamp(e.cursorX, 0, e.screenW-1)
		case REL_Y:
			e.cursorY += int(ev.Value)
			e.cursorY = clamp(e.cursorY, 0, e.screenH-1)
		}

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
			log.Printf("[evdev] Edge %q hit - switching to remote", e.Edge)
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
			wheel = int(ev.Value) * 120
		}
		if e.OnMouseEvent != nil && (dx != 0 || dy != 0 || wheel != 0) {
			e.OnMouseEvent(dx, dy, wheel)
		}

	case EV_KEY:
		if ev.Code >= BTN_LEFT && ev.Code <= BTN_MIDDLE && e.OnButtonEvent != nil {
			e.OnButtonEvent(ev.Code, ev.Value == 1)
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

func (e *EvdevCapture) Close() {
	e.Ungrab()
	if e.mouseFile != nil {
		e.mouseFile.Close()
	}
	if e.kbdFile != nil {
		e.kbdFile.Close()
	}
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
