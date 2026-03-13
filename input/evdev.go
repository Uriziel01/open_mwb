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
	"time"
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

func eviocgphys(length int) uintptr {
	return uintptr(2<<30 | uintptr(length)<<16 | 'E'<<8 | 0x07)
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
	Phys         string
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
	// 3. And it has at least one mouse button (BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, BTN_SIDE, BTN_EXTRA)

	if hasBit(caps.EvBits[:], EV_REL) {
		hasRelX := hasBit(caps.RelBits[:], REL_X)
		hasRelY := hasBit(caps.RelBits[:], REL_Y)

		// Some mice expose movement and buttons on separate interfaces.
		// Accept REL_X/REL_Y movement-only interfaces too.
		if hasRelX || hasRelY {
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
		phys := devicePhys(f.Fd())

		// Classify device
		isMouse, isKeyboard, isTouchpad := classifyDevice(caps)

		info := DeviceInfo{
			Path:         path,
			Name:         name,
			Phys:         phys,
			Capabilities: caps,
			IsMouse:      isMouse,
			IsKeyboard:   isKeyboard,
			IsTouchpad:   isTouchpad,
		}

		if isMouse && !isTouchpad {
			mice = append(mice, info)
			if phys != "" {
				log.Printf("[evdev] Found mouse: %s (%s) phys=%s", path, name, phys)
			} else {
				log.Printf("[evdev] Found mouse: %s (%s)", path, name)
			}
		}
		if isKeyboard {
			keyboards = append(keyboards, info)
			if phys != "" {
				log.Printf("[evdev] Found keyboard: %s (%s) phys=%s", path, name, phys)
			} else {
				log.Printf("[evdev] Found keyboard: %s (%s)", path, name)
			}
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
	IsRemote       bool
	OnEdgeHit      func()
	OnReturn       func()
	OnMouseEvent   func(dx, dy, wheelDelta int)
	OnKeyEvent     func(code uint16, pressed bool)
	OnButtonEvent  func(code uint16, pressed bool)
	OnEmergency    func() // Called when PAUSE is pressed (emergency kill switch)
	pressedKeys    map[uint16]bool
	pendingMouse   *DeviceHandle
	pendingHits    int
	pendingMotion  int
	pendingUntil   time.Time
}

type mouseActivationAction int

const (
	mouseActionNone mouseActivationAction = iota
	mouseActionActivate
	mouseActionRebind
)

func NewEvdevCapture(screenW, screenH int) *EvdevCapture {
	return &EvdevCapture{
		screenW:     screenW,
		screenH:     screenH,
		cursorX:     screenW / 2,
		cursorY:     screenH / 2,
		pressedKeys: make(map[uint16]bool),
	}
}

const (
	mouseIntentWindow             = 150 * time.Millisecond
	mouseIntentMinMotionMagnitude = 4
)

func deviceName(fd uintptr, fallback string) string {
	nameBuf := make([]byte, 256)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgname(len(nameBuf)),
		uintptr(unsafe.Pointer(&nameBuf[0])))
	if errno != 0 {
		return fallback
	}
	name := strings.TrimRight(string(nameBuf), "\x00")
	if name == "" {
		return fallback
	}
	return name
}

func devicePhys(fd uintptr) string {
	physBuf := make([]byte, 256)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, eviocgphys(len(physBuf)),
		uintptr(unsafe.Pointer(&physBuf[0])))
	if errno != 0 {
		return ""
	}
	return strings.TrimRight(string(physBuf), "\x00")
}

func normalizeDeviceName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func normalizeDevicePhys(phys string) string {
	phys = strings.TrimSpace(phys)
	if phys == "" {
		return ""
	}
	if idx := strings.LastIndex(phys, "/input"); idx != -1 {
		suffix := phys[idx+len("/input"):]
		allDigits := suffix != ""
		for _, r := range suffix {
			if r < '0' || r > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			phys = phys[:idx]
		}
	}
	return strings.ToLower(phys)
}

// Open opens explicit input device paths for mouse and keyboard.
func (e *EvdevCapture) Open(mousePath, kbdPath string) error {
	mouseFile, err := os.OpenFile(mousePath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open mouse %s: %w", mousePath, err)
	}

	kbdFile, err := os.OpenFile(kbdPath, os.O_RDONLY, 0)
	if err != nil {
		mouseFile.Close()
		return fmt.Errorf("open keyboard %s: %w", kbdPath, err)
	}

	e.mouseDevs = []*DeviceHandle{
		{
			File: mouseFile,
			Info: DeviceInfo{
				Path: mousePath,
				Name: deviceName(mouseFile.Fd(), filepath.Base(mousePath)),
				Phys: devicePhys(mouseFile.Fd()),
			},
		},
	}
	e.kbdDevs = []*DeviceHandle{
		{
			File: kbdFile,
			Info: DeviceInfo{
				Path: kbdPath,
				Name: deviceName(kbdFile.Fd(), filepath.Base(kbdPath)),
				Phys: devicePhys(kbdFile.Fd()),
			},
		},
	}

	log.Printf("[evdev] Using configured mouse: %s (%s)", mousePath, e.mouseDevs[0].Info.Name)
	log.Printf("[evdev] Using configured keyboard: %s (%s)", kbdPath, e.kbdDevs[0].Info.Name)

	return nil
}

func (e *EvdevCapture) DiscoverAndOpen() error {
	mice, keyboards, _, err := discoverDevices()
	if err != nil {
		return err
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

	// Grab the specific mouse that triggered the transition (if any)
	// If activeMouse is nil, we're switching via keyboard shortcut and will
	// wait for the first pointer event to determine which mouse to grab
	if activeMouse != nil && !activeMouse.Grabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, activeMouse.File.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			log.Printf("[evdev] Warning: failed to grab mouse %s: %v", activeMouse.Info.Path, errno)
		} else {
			activeMouse.Grabbed = true
			e.activeMouseDev = activeMouse
			log.Printf("[evdev] Grabbed mouse: %s", activeMouse.Info.Name)
		}
	} else if activeMouse == nil {
		// When switching via keyboard, don't grab any mouse yet
		// The mouse loop will grab the first intentional REL movement/button event.
		e.resetPendingMouseLocked()
		log.Printf("[evdev] Switching via keyboard - waiting for intentional REL mouse input to select active mouse")
	}

	// Grab keyboard for remote typing
	for _, dev := range e.kbdDevs {
		if e.isMouseDevicePathLocked(dev.Info.Path) {
			log.Printf("[evdev] Skipping keyboard grab for combo mouse device: %s (%s)", dev.Info.Name, dev.Info.Path)
			continue
		}

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

func (e *EvdevCapture) isMouseDevicePathLocked(path string) bool {
	for _, dev := range e.mouseDevs {
		if dev.Info.Path == path {
			return true
		}
	}
	return false
}

func isSamePhysicalDevice(a, b DeviceInfo) bool {
	physA := normalizeDevicePhys(a.Phys)
	physB := normalizeDevicePhys(b.Phys)
	if physA != "" && physA == physB {
		return true
	}

	nameA := normalizeDeviceName(a.Name)
	nameB := normalizeDeviceName(b.Name)
	return nameA != "" && nameA == nameB
}

func (e *EvdevCapture) mouseSourceForKeyboardButtonLocked(kbd *DeviceHandle) *DeviceHandle {
	// If this exact path is already in mouseDevs, mouse loop handles these events.
	if e.isMouseDevicePathLocked(kbd.Info.Path) {
		return nil
	}

	var matches []*DeviceHandle
	for _, mouse := range e.mouseDevs {
		if isSamePhysicalDevice(kbd.Info, mouse.Info) {
			matches = append(matches, mouse)
		}
	}

	switch len(matches) {
	case 0:
		return nil
	case 1:
		return matches[0]
	default:
		// If the active mouse is one of the candidates, preserve it.
		for _, mouse := range matches {
			if mouse == e.activeMouseDev {
				return mouse
			}
		}
		return matches[0]
	}
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
	e.resetPendingMouseLocked()
	// Reset cursor to center
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
		if isRemote {
			switch e.decideMouseActivationLocked(dev, ev) {
			case mouseActionActivate:
				e.grabMouseLocked(dev, "initial activation")
			case mouseActionRebind:
				e.ungrabMouseLocked(e.activeMouseDev, "adaptive rebind")
				e.grabMouseLocked(dev, "adaptive rebind")
			}
		}
		activeMouse := e.activeMouseDev
		e.mu.Unlock()

		if ev.Type == EV_KEY && isMouseButtonCode(ev.Code) {
			logMouseButtonCapture(dev, ev, isRemote, activeMouse)
		}

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

func isMouseButtonPress(ev InputEvent) bool {
	return ev.Type == EV_KEY && ev.Value == 1 && isMouseButtonCode(ev.Code)
}

func (e *EvdevCapture) resetPendingMouseLocked() {
	e.pendingMouse = nil
	e.pendingHits = 0
	e.pendingMotion = 0
	e.pendingUntil = time.Time{}
}

func absInt(v int32) int {
	if v < 0 {
		return int(-v)
	}
	return int(v)
}

func (e *EvdevCapture) registerPendingMouseLocked(source *DeviceHandle, ev InputEvent) {
	now := time.Now()
	if e.pendingMouse != source || now.After(e.pendingUntil) {
		e.pendingMouse = source
		e.pendingHits = 1
		e.pendingMotion = absInt(ev.Value)
		e.pendingUntil = now.Add(mouseIntentWindow)
		log.Printf("[evdev] Mouse candidate observed: %s", source.Info.Name)
		return
	}

	e.pendingHits++
	e.pendingMotion += absInt(ev.Value)
	e.pendingUntil = now.Add(mouseIntentWindow)
}

func (e *EvdevCapture) decideMouseActivationLocked(source *DeviceHandle, ev InputEvent) mouseActivationAction {
	base := remoteMouseActivationAction(e.activeMouseDev, source, ev)
	if base == mouseActionNone {
		if !time.Now().Before(e.pendingUntil) {
			e.resetPendingMouseLocked()
		}
		return mouseActionNone
	}

	if isMouseButtonPress(ev) {
		e.resetPendingMouseLocked()
		return base
	}

	// Require two consecutive intentional motion events from the same device
	// to avoid accidental activation/rebind from a single noisy packet.
	e.registerPendingMouseLocked(source, ev)
	if e.pendingHits < 2 || e.pendingMotion < mouseIntentMinMotionMagnitude {
		return mouseActionNone
	}

	e.resetPendingMouseLocked()
	return base
}

func isMouseButtonCode(code uint16) bool {
	return code >= BTN_MOUSE && code <= BTN_MOUSE+0x0f
}

func mouseButtonName(code uint16) string {
	switch code {
	case BTN_LEFT:
		return "LEFT"
	case BTN_RIGHT:
		return "RIGHT"
	case BTN_MIDDLE:
		return "MIDDLE"
	case BTN_SIDE:
		return "SIDE"
	case BTN_EXTRA:
		return "EXTRA"
	default:
		return fmt.Sprintf("BTN_%d", code)
	}
}

func mouseButtonAction(value int32) string {
	switch value {
	case 1:
		return "DOWN"
	case 0:
		return "UP"
	case 2:
		return "REPEAT"
	default:
		return fmt.Sprintf("VALUE_%d", value)
	}
}

func logMouseButtonCapture(dev *DeviceHandle, ev InputEvent, isRemote bool, activeMouse *DeviceHandle) {
	route := "LOCAL_ONLY"
	switch {
	case isRemote && activeMouse == nil:
		route = "REMOTE_WAITING_ACTIVE_MOUSE"
	case isRemote && dev == activeMouse:
		route = "REMOTE_FORWARDED"
	case isRemote && dev != activeMouse:
		route = "REMOTE_IGNORED_INACTIVE_MOUSE"
	}

	log.Printf("[LOCAL-MOUSE] %s %s | Device=%s (%s) | Route=%s",
		mouseButtonName(ev.Code), mouseButtonAction(ev.Value), dev.Info.Name, dev.Info.Path, route)
}

func logMouseButtonCaptureKeyboardPath(dev *DeviceHandle, ev InputEvent, route string, sourceMouse *DeviceHandle) {
	if sourceMouse != nil {
		log.Printf("[LOCAL-MOUSE] %s %s | Device=%s (%s) | Route=%s | MappedMouse=%s (%s)",
			mouseButtonName(ev.Code), mouseButtonAction(ev.Value), dev.Info.Name, dev.Info.Path, route,
			sourceMouse.Info.Name, sourceMouse.Info.Path)
		return
	}

	log.Printf("[LOCAL-MOUSE] %s %s | Device=%s (%s) | Route=%s",
		mouseButtonName(ev.Code), mouseButtonAction(ev.Value), dev.Info.Name, dev.Info.Path, route)
}

func isIntentionalRemoteMouseEvent(ev InputEvent) bool {
	// REL-only selection policy: ignore EV_ABS devices for activation/rebind.
	if ev.Type == EV_REL {
		switch ev.Code {
		case REL_X, REL_Y:
			return ev.Value != 0
		}
		return false
	}

	// Treat mouse button presses as intentional selection/rebind input.
	if ev.Type == EV_KEY && ev.Value == 1 && isMouseButtonCode(ev.Code) {
		return true
	}

	return false
}

func remoteMouseActivationAction(active, source *DeviceHandle, ev InputEvent) mouseActivationAction {
	if !isIntentionalRemoteMouseEvent(ev) {
		return mouseActionNone
	}
	if active == nil {
		return mouseActionActivate
	}
	if active != source {
		return mouseActionRebind
	}
	return mouseActionNone
}

func (e *EvdevCapture) ungrabMouseLocked(dev *DeviceHandle, reason string) {
	if dev == nil || !dev.Grabbed {
		return
	}
	unix.Syscall(unix.SYS_IOCTL, dev.File.Fd(), EVIOCGRAB, 0)
	dev.Grabbed = false
	log.Printf("[evdev] Released active mouse (%s): %s", reason, dev.Info.Name)
}

func (e *EvdevCapture) grabMouseLocked(dev *DeviceHandle, reason string) {
	if dev == nil {
		return
	}

	e.activeMouseDev = dev
	if !dev.Grabbed {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, dev.File.Fd(), EVIOCGRAB, 1)
		if errno != 0 {
			log.Printf("[evdev] Warning: failed to grab mouse %s during %s: %v", dev.Info.Path, reason, errno)
		} else {
			dev.Grabbed = true
		}
	}

	log.Printf("[evdev] Active mouse selected (%s): %s (%s)", reason, dev.Info.Name, dev.Info.Path)
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

		// Skip auto-repeat events (ev.Value == 2) to prevent keys from getting stuck
		// Only process actual key press (1) and key release (0) events
		if ev.Value == 2 {
			continue
		}

		// Some combined HID receivers expose mouse buttons on keyboard-like devices.
		// Keep these out of keyboard forwarding and map them back to the matching mouse.
		if isMouseButtonCode(ev.Code) {
			pressed := ev.Value == 1
			route := "LOCAL_KEYBOARD_PATH"
			var sourceMouse *DeviceHandle
			shouldForward := false

			e.mu.Lock()
			if e.IsRemote {
				sourceMouse = e.mouseSourceForKeyboardButtonLocked(dev)
				if sourceMouse == nil {
					route = "REMOTE_KEYBOARD_PATH_FILTERED"
				} else {
					switch e.decideMouseActivationLocked(sourceMouse, ev) {
					case mouseActionActivate:
						e.grabMouseLocked(sourceMouse, "keyboard-path mouse button")
					case mouseActionRebind:
						e.ungrabMouseLocked(e.activeMouseDev, "keyboard-path adaptive rebind")
						e.grabMouseLocked(sourceMouse, "keyboard-path adaptive rebind")
					}

					if e.activeMouseDev == sourceMouse {
						route = "REMOTE_FORWARDED_KEYBOARD_COMBO"
						shouldForward = true
					} else {
						route = "REMOTE_IGNORED_INACTIVE_MOUSE"
					}
				}
			}
			e.mu.Unlock()

			if shouldForward && e.OnButtonEvent != nil {
				e.OnButtonEvent(ev.Code, pressed)
			}
			logMouseButtonCaptureKeyboardPath(dev, ev, route, sourceMouse)
			continue
		}
		pressed := ev.Value == 1

		// Track pressed keys globally on ALL keyboards (both local and remote mode)
		// This is needed for global shortcuts like Win+F1/F2
		e.mu.Lock()
		if pressed {
			e.pressedKeys[ev.Code] = true
		} else {
			delete(e.pressedKeys, ev.Code)
		}
		isRemote := e.IsRemote
		activeKbd := e.activeKbdDev
		e.mu.Unlock()

		// Handle emergency button (PAUSE) on ALL keyboards in both local and remote mode
		// This ensures the emergency button works even when devices are grabbed
		if ev.Code == 119 && ev.Value == 1 { // PAUSE pressed
			log.Println("[evdev] PAUSE (emergency) detected on keyboard")
			if e.OnEmergency != nil {
				e.OnEmergency()
			}
		}

		// Check machine switching shortcuts on ALL keyboards in remote mode
		// This ensures Win+F1/F2 work regardless of which keyboard sent the event
		if isRemote {
			// Win+F1 to switch to Machine 1 (Windows)
			if ev.Code == 59 && ev.Value == 1 { // F1 pressed
				e.mu.Lock()
				_, hasWin := e.pressedKeys[125] // Left Win key (KEY_LEFTMETA)
				if !hasWin {
					_, hasWin = e.pressedKeys[126] // Right Win key (KEY_RIGHTMETA)
				}
				e.mu.Unlock()

				if hasWin {
					log.Println("[evdev] Win+F1 - switching to Machine 1 (Windows)")
					e.Ungrab()
					if e.OnReturn != nil {
						e.OnReturn()
					}
					continue
				}
			}

			// Win+F2 to switch to Machine 2 (Linux)
			if ev.Code == 60 && ev.Value == 1 { // F2 pressed
				e.mu.Lock()
				_, hasWin := e.pressedKeys[125] // Left Win key (KEY_LEFTMETA)
				if !hasWin {
					_, hasWin = e.pressedKeys[126] // Right Win key (KEY_RIGHTMETA)
				}
				e.mu.Unlock()

				if hasWin {
					log.Println("[evdev] Win+F2 - switching to Machine 2 (Linux)")
					e.Ungrab()
					if e.OnReturn != nil {
						e.OnReturn()
					}
					continue
				}
			}
		}

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

			if e.OnKeyEvent != nil {
				e.OnKeyEvent(ev.Code, pressed)
			}
		} else {
			// Local mode: Check Win+F1/F2 to switch to remote
			if ev.Code == 59 && ev.Value == 1 { // F1 pressed
				e.mu.Lock()
				_, hasWin := e.pressedKeys[125] // Left Win key (KEY_LEFTMETA)
				if !hasWin {
					_, hasWin = e.pressedKeys[126] // Right Win key (KEY_RIGHTMETA)
				}
				e.mu.Unlock()

				if hasWin {
					log.Println("[evdev] Win+F1 - entering remote mode (Machine 1)")
					// When switching via keyboard, don't grab any mouse yet
					// Wait for the first pointer event to determine which mouse to use
					e.Grab(nil)
					if e.OnEdgeHit != nil {
						e.OnEdgeHit()
					}
				}
			}

			if ev.Code == 60 && ev.Value == 1 { // F2 pressed
				e.mu.Lock()
				_, hasWin := e.pressedKeys[125] // Left Win key (KEY_LEFTMETA)
				if !hasWin {
					_, hasWin = e.pressedKeys[126] // Right Win key (KEY_RIGHTMETA)
				}
				e.mu.Unlock()

				if hasWin {
					log.Println("[evdev] Win+F2 - entering remote mode (Machine 2)")
					// When switching via keyboard, don't grab any mouse yet
					// Wait for the first pointer event to determine which mouse to use
					e.Grab(nil)
					if e.OnEdgeHit != nil {
						e.OnEdgeHit()
					}
				}
			}
		}
	}
}

func (e *EvdevCapture) handleLocalMouseEvent(ev InputEvent, dev *DeviceHandle) {
	// Local mode: just track mouse movement for local use
	// No edge detection - we use Win+F1/F2 shortcuts for switching
	if ev.Type == EV_REL {
		e.mu.Lock()
		switch ev.Code {
		case REL_X:
			e.cursorX += int(ev.Value)
		case REL_Y:
			e.cursorY += int(ev.Value)
		}
		e.mu.Unlock()
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
		// Ignore hold/repeat values to avoid false button-up during drag.
		// Valid button states are press=1 and release=0.
		if ev.Value != 0 && ev.Value != 1 {
			return
		}

		// Handle all mouse buttons (1-5): LEFT, RIGHT, MIDDLE, SIDE, EXTRA
		if e.OnButtonEvent != nil {
			switch ev.Code {
			case BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, BTN_SIDE, BTN_EXTRA:
				e.OnButtonEvent(ev.Code, ev.Value == 1)
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

// applyAcceleration adds a simple non-linear acceleration curve to raw mouse movements.
// This helps the simulated cursor position stay in sync with the OS's accelerated cursor.
func applyAcceleration(val int) int {
	absVal := val
	if absVal < 0 {
		absVal = -absVal
	}

	// Fast movement: increase speed significantly
	if absVal > 15 {
		return val * 3
	}
	// Medium movement: slight acceleration
	if absVal > 5 {
		return val * 2
	}
	// Slow movement: raw 1:1 speed
	return val
}
