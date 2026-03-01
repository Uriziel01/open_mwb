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
	EV_SYN       = 0x00
	EV_KEY       = 0x01
	EV_REL       = 0x02
	EV_ABS       = 0x03
	REL_X        = 0x00
	REL_Y        = 0x01
	REL_WHEEL    = 0x08
	REL_HWHEEL   = 0x06
	ABS_PRESSURE = 0x18
	BTN_MOUSE    = 0x110
	// Note: ABS_X, ABS_Y, BTN_LEFT, BTN_RIGHT, BTN_MIDDLE are defined in uinput.go
)

const EVIOCGRAB = 0x40044590

func eviocgname(length int) uintptr {
	return uintptr(2<<30 | uintptr(length)<<16 | 'E'<<8 | 0x06)
}

func eviocgbit(ev, length int) uintptr {
	return uintptr(2<<30) | uintptr(length)<<16 | uintptr('E')<<8 | uintptr(0x20+ev)
}

// DeviceCapabilities holds the capability bits for a device
type DeviceCapabilities struct {
	EvBits  [8]byte  // Event types (EV_* constants)
	RelBits [8]byte  // Relative events (REL_* constants)
	AbsBits [8]byte  // Absolute events (ABS_* constants)
	KeyBits [64]byte // Key/button events (KEY_*, BTN_* constants)
}

// DeviceInfo holds information about an input device
type DeviceInfo struct {
	Path         string
	Name         string
	Capabilities DeviceCapabilities
	IsMouse      bool
	IsKeyboard   bool
	IsTouchpad   bool
}

// detectCapabilities queries a device for its capabilities
func detectCapabilities(fd uintptr) (DeviceCapabilities, error) {
	var caps DeviceCapabilities

	// Get event type bits (0 = EV_SYN to 7 = EV_MAX)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgbit(0, len(caps.EvBits)),
		uintptr(unsafe.Pointer(&caps.EvBits[0])))
	if errno != 0 {
		return caps, fmt.Errorf("EVIOCGBIT(0): %v", errno)
	}

	// Get relative event bits
	if hasBit(caps.EvBits[:], EV_REL) {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgbit(EV_REL, len(caps.RelBits)),
			uintptr(unsafe.Pointer(&caps.RelBits[0])))
		if errno != 0 {
			return caps, fmt.Errorf("EVIOCGBIT(EV_REL): %v", errno)
		}
	}

	// Get absolute event bits
	if hasBit(caps.EvBits[:], EV_ABS) {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgbit(EV_ABS, len(caps.AbsBits)),
			uintptr(unsafe.Pointer(&caps.AbsBits[0])))
		if errno != 0 {
			return caps, fmt.Errorf("EVIOCGBIT(EV_ABS): %v", errno)
		}
	}

	// Get key event bits
	if hasBit(caps.EvBits[:], EV_KEY) {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgbit(EV_KEY, len(caps.KeyBits)),
			uintptr(unsafe.Pointer(&caps.KeyBits[0])))
		if errno != 0 {
			return caps, fmt.Errorf("EVIOCGBIT(EV_KEY): %v", errno)
		}
	}

	return caps, nil
}

// hasBit checks if a specific bit is set in a bit array
func hasBit(bits []byte, bit int) bool {
	if bit/8 >= len(bits) {
		return false
	}
	return bits[bit/8]&(1<<(bit%8)) != 0
}

// classifyDevice determines if a device is a mouse, keyboard, or touchpad
func classifyDevice(caps DeviceCapabilities) (isMouse, isKeyboard, isTouchpad bool) {
	// A device is a mouse if:
	// 1. It has EV_REL and (REL_X or REL_Y) - relative movement
	// 2. Or it has EV_ABS and (ABS_X and ABS_Y) - absolute movement (touchscreens, tablets)
	// 3. And it has at least one mouse button (BTN_LEFT, BTN_RIGHT, BTN_MIDDLE)

	if hasBit(caps.EvBits[:], EV_REL) {
		hasRelX := hasBit(caps.RelBits[:], REL_X)
		hasRelY := hasBit(caps.RelBits[:], REL_Y)
		hasButtons := hasBit(caps.KeyBits[:], BTN_LEFT) ||
			hasBit(caps.KeyBits[:], BTN_RIGHT) ||
			hasBit(caps.KeyBits[:], BTN_MIDDLE)
		hasMouseButton := hasBit(caps.KeyBits[:], BTN_MOUSE)

		if (hasRelX || hasRelY) && (hasButtons || hasMouseButton) {
			isMouse = true
		}
	}

	// Check for absolute positioning devices (touchpads, touchscreens)
	if hasBit(caps.EvBits[:], EV_ABS) {
		hasAbsX := hasBit(caps.AbsBits[:], ABS_X)
		hasAbsY := hasBit(caps.AbsBits[:], ABS_Y)
		hasPressure := hasBit(caps.AbsBits[:], ABS_PRESSURE)
		hasButtons := hasBit(caps.KeyBits[:], BTN_LEFT)

		// Touchpad: absolute X/Y + pressure + left button
		if hasAbsX && hasAbsY && hasPressure && hasButtons {
			isTouchpad = true
			// Touchpads often report as mice too, so mark as both
			isMouse = true
		}
	}

	// A device is a keyboard if it has EV_KEY and substantial typing keys
	// Require multiple essential typing keys to filter out multimedia key devices
	if hasBit(caps.EvBits[:], EV_KEY) {
		// Count actual typing keys (letters, numbers, function keys)
		// Key codes: KEY_1=2 to KEY_0=11 (numbers), KEY_A=30 to KEY_Z=45 (letters)
		// KEY_F1=59 to KEY_F12=70, KEY_SPACE=57, KEY_ENTER=28
		typingKeys := 0
		
		// Check for letters A-Z (codes 30-45)
		for i := 30; i <= 45; i++ {
			if hasBit(caps.KeyBits[:], i) {
				typingKeys++
			}
		}
		
		// Check for numbers 1-0 (codes 2-11)
		for i := 2; i <= 11; i++ {
			if hasBit(caps.KeyBits[:], i) {
				typingKeys++
			}
		}
		
		// Check for function keys F1-F12 (codes 59-70)
		for i := 59; i <= 70; i++ {
			if hasBit(caps.KeyBits[:], i) {
				typingKeys++
			}
		}
		
		// Also check for essential keys
		hasSpace := hasBit(caps.KeyBits[:], 57)
		hasEnter := hasBit(caps.KeyBits[:], 28)
		
		// Must have substantial typing capability
		// Require at least 15 typing keys (roughly half of A-Z) plus essential keys
		if typingKeys >= 15 && hasSpace && hasEnter {
			isKeyboard = true
		}
	}

	return
}

// discoverDevices finds all input devices and classifies them by capability
func discoverDevices() ([]DeviceInfo, []DeviceInfo, []DeviceInfo, error) {
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("glob devices: %w", err)
	}

	var mice []DeviceInfo
	var keyboards []DeviceInfo
	var touchpads []DeviceInfo

	for _, path := range matches {
		f, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			continue
		}

		// Get device name
		nameBuf := make([]byte, 256)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), eviocgname(len(nameBuf)),
			uintptr(unsafe.Pointer(&nameBuf[0])))
		if errno != 0 {
			f.Close()
			continue
		}
		name := strings.TrimRight(string(nameBuf), "\x00")

		// Get capabilities
		caps, err := detectCapabilities(f.Fd())
		if err != nil {
			f.Close()
			continue
		}

		// Classify device
		isMouse, isKeyboard, isTouchpad := classifyDevice(caps)

		info := DeviceInfo{
			Path:         path,
			Name:         name,
			Capabilities: caps,
			IsMouse:      isMouse,
			IsKeyboard:   isKeyboard,
			IsTouchpad:   isTouchpad,
		}

		if isMouse && !isTouchpad {
			mice = append(mice, info)
			log.Printf("[evdev] Found mouse: %s (%s)", path, name)
		}
		if isKeyboard {
			keyboards = append(keyboards, info)
			log.Printf("[evdev] Found keyboard: %s (%s)", path, name)
		}
		if isTouchpad {
			touchpads = append(touchpads, info)
			log.Printf("[evdev] Found touchpad: %s (%s)", path, name)
		}

		f.Close()
	}

	return mice, keyboards, touchpads, nil
}

type DeviceHandle struct {
	File    *os.File
	Info    DeviceInfo
	Grabbed bool
}

type EvdevCapture struct {
	mu             sync.Mutex
	mouseDevs      []*DeviceHandle
	kbdDevs        []*DeviceHandle
	activeMouseDev *DeviceHandle // The mouse that triggered edge transition
	activeKbdDev   *DeviceHandle // The keyboard that was first used in remote mode
	cursorX        int
	cursorY        int
	screenW        int
	screenH        int
	Edge           string
	IsRemote       bool
	OnEdgeHit      func()
	OnReturn       func()
	OnMouseEvent   func(dx, dy, wheelDelta int)
	OnKeyEvent     func(code uint16, pressed bool)
	OnButtonEvent  func(code uint16, pressed bool)
	pressedKeys    map[uint16]bool
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

func (e *EvdevCapture) DiscoverAndOpen() error {
	mice, keyboards, touchpads, err := discoverDevices()
	if err != nil {
		return err
	}

	// Use touchpads as mice if no dedicated mice found
	if len(mice) == 0 && len(touchpads) > 0 {
		mice = touchpads
		log.Printf("[evdev] Using %d touchpad(s) as mouse input", len(touchpads))
	}

	if len(mice) == 0 {
		return fmt.Errorf("no mouse devices found")
	}

	if len(keyboards) == 0 {
		return fmt.Errorf("no keyboard devices found")
	}

	// Open all mouse devices
	for _, info := range mice {
		f, err := os.OpenFile(info.Path, os.O_RDONLY, 0)
		if err != nil {
			log.Printf("[evdev] Warning: failed to open mouse %s: %v", info.Path, err)
			continue
		}
		e.mouseDevs = append(e.mouseDevs, &DeviceHandle{
			File: f,
			Info: info,
		})
	}

	// Open all keyboard devices
	for _, info := range keyboards {
		f, err := os.OpenFile(info.Path, os.O_RDONLY, 0)
		if err != nil {
			log.Printf("[evdev] Warning: failed to open keyboard %s: %v", info.Path, err)
			continue
		}
		e.kbdDevs = append(e.kbdDevs, &DeviceHandle{
			File: f,
			Info: info,
		})
	}

	if len(e.mouseDevs) == 0 {
		return fmt.Errorf("failed to open any mouse devices")
	}

	if len(e.kbdDevs) == 0 {
		return fmt.Errorf("failed to open any keyboard devices")
	}

	log.Printf("[evdev] Using %d mouse device(s) and %d keyboard device(s)",
		len(e.mouseDevs), len(e.kbdDevs))

	return nil
}

func FindMouseDevice() (string, error) {
	mice, _, _, err := discoverDevices()
	if err != nil {
		return "", err
	}
	if len(mice) == 0 {
		return "", fmt.Errorf("no mouse devices found")
	}
	return mice[0].Path, nil
}

func FindKeyboardDevice() (string, error) {
	_, keyboards, _, err := discoverDevices()
	if err != nil {
		return "", err
	}
	if len(keyboards) == 0 {
		return "", fmt.Errorf("no keyboard devices found")
	}
	return keyboards[0].Path, nil
}

func ListDevices() {
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		f, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			continue
		}

		nameBuf := make([]byte, 256)
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), eviocgname(len(nameBuf)),
			uintptr(unsafe.Pointer(&nameBuf[0])))

		var caps DeviceCapabilities
		var capsErr error
		if errno == 0 {
			caps, capsErr = detectCapabilities(f.Fd())
		}

		f.Close()

		if errno != 0 || capsErr != nil {
			continue
		}

		name := strings.TrimRight(string(nameBuf), "\x00")
		isMouse, isKeyboard, isTouchpad := classifyDevice(caps)

		types := []string{}
		if isMouse {
			types = append(types, "mouse")
		}
		if isKeyboard {
			types = append(types, "keyboard")
		}
		if isTouchpad {
			types = append(types, "touchpad")
		}

		if len(types) > 0 {
			fmt.Printf("  %s: %s [%s]\n", path, name, strings.Join(types, ", "))
		}
	}
}

func (e *EvdevCapture) Grab(activeMouse *DeviceHandle) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Only grab the specific mouse that triggered the transition
	if activeMouse != nil && !activeMouse.Grabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, activeMouse.File.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			log.Printf("[evdev] Warning: failed to grab mouse %s: %v", activeMouse.Info.Path, errno)
		} else {
			activeMouse.Grabbed = true
			e.activeMouseDev = activeMouse
			log.Printf("[evdev] Grabbed mouse: %s", activeMouse.Info.Name)
		}
	}

	// Grab keyboard for remote typing
	for _, dev := range e.kbdDevs {
		if !dev.Grabbed {
			_, _, errno := unix.Syscall(unix.SYS_IOCTL, dev.File.Fd(), EVIOCGRAB, 1)
			if errno != 0 {
				log.Printf("[evdev] Warning: failed to grab keyboard %s: %v", dev.Info.Path, errno)
				continue
			}
			dev.Grabbed = true
		}
	}

	e.IsRemote = true

	// Release all pressed keys AFTER grabbing and setting remote mode
	for code := range e.pressedKeys {
		if e.OnKeyEvent != nil {
			e.OnKeyEvent(code, false)
		}
		delete(e.pressedKeys, code)
	}

	log.Println("[evdev] Remote mode active - other mice remain free for local control")
	return nil
}

func (e *EvdevCapture) Ungrab() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Ungrab all mouse devices
	for _, dev := range e.mouseDevs {
		if dev.Grabbed {
			unix.Syscall(unix.SYS_IOCTL, dev.File.Fd(), EVIOCGRAB, 0)
			dev.Grabbed = false
		}
	}

	// Ungrab all keyboard devices
	for _, dev := range e.kbdDevs {
		if dev.Grabbed {
			unix.Syscall(unix.SYS_IOCTL, dev.File.Fd(), EVIOCGRAB, 0)
			dev.Grabbed = false
		}
	}

	e.IsRemote = false
	e.activeMouseDev = nil
	e.activeKbdDev = nil
	e.cursorX = e.screenW / 2
	e.cursorY = e.screenH / 2
	log.Println("[evdev] Released all devices - local mode")
	return nil
}

func (e *EvdevCapture) RunMouseLoop() {
	// Start a goroutine for each mouse device
	var wg sync.WaitGroup
	for _, dev := range e.mouseDevs {
		wg.Add(1)
		go func(handle *DeviceHandle) {
			defer wg.Done()
			e.runSingleMouseLoop(handle)
		}(dev)
	}
	wg.Wait()
}

func (e *EvdevCapture) runSingleMouseLoop(dev *DeviceHandle) {
	buf := make([]byte, inputEventSize)
	for {
		_, err := dev.File.Read(buf)
		if err != nil {
			return
		}

		ev := parseEvent(buf)

		e.mu.Lock()
		isRemote := e.IsRemote
		activeMouse := e.activeMouseDev
		e.mu.Unlock()

		if isRemote {
			// In remote mode, only process events from the active mouse
			if dev == activeMouse {
				e.handleRemoteMouseEvent(ev)
			}
		} else {
			e.handleLocalMouseEvent(ev, dev)
		}
	}
}

func (e *EvdevCapture) RunKeyboardLoop() {
	// Start a goroutine for each keyboard device
	var wg sync.WaitGroup
	for _, dev := range e.kbdDevs {
		wg.Add(1)
		go func(handle *DeviceHandle) {
			defer wg.Done()
			e.runSingleKeyboardLoop(handle)
		}(dev)
	}
	wg.Wait()
}

func (e *EvdevCapture) runSingleKeyboardLoop(dev *DeviceHandle) {
	buf := make([]byte, inputEventSize)
	for {
		_, err := dev.File.Read(buf)
		if err != nil {
			return
		}

		ev := parseEvent(buf)

		if ev.Type != EV_KEY {
			continue
		}

		pressed := ev.Value == 1 || ev.Value == 2

		e.mu.Lock()
		isRemote := e.IsRemote
		activeKbd := e.activeKbdDev
		e.mu.Unlock()

		if isRemote {
			// In remote mode, only accept events from the active keyboard
			// If no active keyboard is set, make this one active
			if activeKbd == nil && pressed {
				e.mu.Lock()
				e.activeKbdDev = dev
				activeKbd = dev
				e.mu.Unlock()
				log.Printf("[evdev] Active keyboard set: %s", dev.Info.Name)
			}

			// Only process if this is the active keyboard
			if dev != activeKbd {
				continue
			}

			// ScrollLock to return to local mode (works from active keyboard)
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
			// Track pressed keys in local mode (any keyboard)
			e.mu.Lock()
			if pressed {
				e.pressedKeys[ev.Code] = true
			} else {
				delete(e.pressedKeys, ev.Code)
			}
			e.mu.Unlock()

			// Right Shift + Right Ctrl to enter remote mode (alternative to edge detection)
			if ev.Code == 54 && ev.Value == 1 { // Right Shift
				e.mu.Lock()
				_, hasRightCtrl := e.pressedKeys[97] // Right Ctrl (97)
				// Get first mouse for keyboard-triggered transition
				var firstMouse *DeviceHandle
				if len(e.mouseDevs) > 0 {
					firstMouse = e.mouseDevs[0]
				}
				e.mu.Unlock()
				if hasRightCtrl && firstMouse != nil {
					log.Println("[evdev] Right Shift+Ctrl - entering remote mode")
					e.Grab(firstMouse)
					if e.OnEdgeHit != nil {
						e.OnEdgeHit()
					}
				}
			}
		}
	}
}

func (e *EvdevCapture) handleLocalMouseEvent(ev InputEvent, dev *DeviceHandle) {
	if ev.Type == EV_REL {
		e.mu.Lock()
		switch ev.Code {
		case REL_X:
			e.cursorX += int(ev.Value)
			e.cursorX = clamp(e.cursorX, 0, e.screenW-1)
		case REL_Y:
			e.cursorY += int(ev.Value)
			e.cursorY = clamp(e.cursorY, 0, e.screenH-1)
		}

		cursorX := e.cursorX
		cursorY := e.cursorY
		screenW := e.screenW
		screenH := e.screenH
		edge := e.Edge
		e.mu.Unlock()

		edgeHit := false
		switch edge {
		case "right":
			edgeHit = cursorX >= screenW-1
		case "left":
			edgeHit = cursorX <= 0
		case "top":
			edgeHit = cursorY <= 0
		case "bottom":
			edgeHit = cursorY >= screenH-1
		}

		if edgeHit {
			log.Printf("[evdev] Edge %q hit on %s - switching to remote", edge, dev.Info.Name)
			e.Grab(dev)
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

	for _, dev := range e.mouseDevs {
		if dev.File != nil {
			dev.File.Close()
		}
	}
	for _, dev := range e.kbdDevs {
		if dev.File != nil {
			dev.File.Close()
		}
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
