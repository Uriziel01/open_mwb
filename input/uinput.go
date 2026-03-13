//go:build linux
// +build linux

package input

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	UI_SET_EVBIT   = 0x40045564
	UI_SET_KEYBIT  = 0x40045565
	UI_SET_RELBIT  = 0x40045566
	UI_SET_ABSBIT  = 0x40045567
	UI_DEV_CREATE  = 0x5501
	UI_DEV_DESTROY = 0x5502
	UI_DEV_SETUP   = 0x405c5503
	UI_ABS_SETUP   = 0x401c5504
)

const (
	ABS_X      = 0x00
	ABS_Y      = 0x01
	BTN_LEFT   = 0x110
	BTN_RIGHT  = 0x111
	BTN_MIDDLE = 0x112
	BTN_SIDE   = 0x113 // Mouse button 4 (Back)
	BTN_EXTRA  = 0x114 // Mouse button 5 (Forward)
	SYN_REPORT = 0x00
)

type uinputSetup struct {
	ID   inputID
	Name [80]byte
	_    uint32
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

type inputAbsinfo struct {
	Value      int32
	Minimum    int32
	Maximum    int32
	Fuzz       int32
	Flat       int32
	Resolution int32
}

type uinputAbsSetup struct {
	Code    uint16
	_       uint16
	Absinfo inputAbsinfo
}

type VirtualInput struct {
	mouseFile    *os.File
	kbdFile      *os.File
	screenW      int
	screenH      int
	absAvailable bool
	absX         int
	absY         int
	btnLeft      bool
	btnRight     bool
	btnMiddle    bool
	btnSide      bool // Mouse button 4 (Back)
	btnExtra     bool // Mouse button 5 (Forward)
	pressedKeys  map[uint16]bool // Track which keys are currently pressed
}

func NewVirtualInput(screenW, screenH int) (*VirtualInput, error) {
	vi := &VirtualInput{
		screenW:     screenW,
		screenH:     screenH,
		pressedKeys: make(map[uint16]bool),
	}

	var err error

	vi.mouseFile, err = os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open uinput: %w", err)
	}

	if err := vi.setupMouse(); err != nil {
		vi.mouseFile.Close()
		return nil, err
	}

	vi.kbdFile, err = os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		vi.mouseFile.Close()
		return nil, fmt.Errorf("open uinput: %w", err)
	}

	if err := vi.setupKeyboard(); err != nil {
		vi.mouseFile.Close()
		vi.kbdFile.Close()
		return nil, err
	}

	time.Sleep(2 * time.Second)

	log.Printf("[uinput] Virtual devices created (%dx%d)", screenW, screenH)
	return vi, nil
}

func (vi *VirtualInput) setupMouse() error {
	fd := vi.mouseFile.Fd()

	for _, evType := range []uintptr{EV_SYN, EV_KEY, EV_REL} {
		if err := ioctl(fd, UI_SET_EVBIT, evType); err != nil {
			return fmt.Errorf("set EV bit %d: %w", evType, err)
		}
	}

	for _, btn := range []uintptr{BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, BTN_SIDE, BTN_EXTRA} {
		if err := ioctl(fd, UI_SET_KEYBIT, btn); err != nil {
			return fmt.Errorf("set key bit %d: %w", btn, err)
		}
	}

	for _, rel := range []uintptr{REL_X, REL_Y, REL_WHEEL} {
		if err := ioctl(fd, UI_SET_RELBIT, rel); err != nil {
			return fmt.Errorf("set rel bit %d: %w", rel, err)
		}
	}

	vi.absAvailable = vi.setupAbsoluteAxes(fd)

	setup := uinputSetup{
		ID: inputID{Bustype: 0x03, Vendor: 0x1234, Product: 0x5678, Version: 1},
	}
	copy(setup.Name[:], "MWB Virtual Mouse")

	if err := ioctlPtr(fd, UI_DEV_SETUP, unsafe.Pointer(&setup)); err != nil {
		return fmt.Errorf("dev setup: %w", err)
	}

	if err := ioctl(fd, UI_DEV_CREATE, 0); err != nil {
		return fmt.Errorf("dev create: %w", err)
	}

	return nil
}

func (vi *VirtualInput) setupAbsoluteAxes(fd uintptr) bool {
	if err := ioctl(fd, UI_SET_EVBIT, EV_ABS); err != nil {
		return false
	}

	for _, abs := range []uintptr{ABS_X, ABS_Y} {
		if err := ioctl(fd, UI_SET_ABSBIT, abs); err != nil {
			return false
		}
	}

	for i, axis := range []struct {
		code uint16
		max  int32
	}{{ABS_X, int32(vi.screenW - 1)}, {ABS_Y, int32(vi.screenH - 1)}} {
		absSetup := uinputAbsSetup{
			Code:    axis.code,
			Absinfo: inputAbsinfo{Minimum: 0, Maximum: axis.max},
		}
		if err := ioctlPtr(fd, UI_ABS_SETUP, unsafe.Pointer(&absSetup)); err != nil {
			log.Printf("[uinput] Abs axis %d setup failed, using relative mode", i)
			return false
		}
	}

	return true
}

func (vi *VirtualInput) setupKeyboard() error {
	fd := vi.kbdFile.Fd()

	for _, evType := range []uintptr{EV_SYN, EV_KEY} {
		if err := ioctl(fd, UI_SET_EVBIT, evType); err != nil {
			return fmt.Errorf("set EV bit %d: %w", evType, err)
		}
	}

	for key := uintptr(1); key <= 248; key++ {
		ioctl(fd, UI_SET_KEYBIT, key)
	}

	setup := uinputSetup{
		ID: inputID{Bustype: 0x03, Vendor: 0x1234, Product: 0x5679, Version: 1},
	}
	copy(setup.Name[:], "MWB Virtual Keyboard")

	if err := ioctlPtr(fd, UI_DEV_SETUP, unsafe.Pointer(&setup)); err != nil {
		return fmt.Errorf("dev setup: %w", err)
	}

	if err := ioctl(fd, UI_DEV_CREATE, 0); err != nil {
		return fmt.Errorf("dev create: %w", err)
	}

	return nil
}

func (vi *VirtualInput) InjectKeyboard(vk int32, flags int32) {
	linuxCode, ok := VKToLinux[vk]
	if !ok {
		return
	}

	// Network protocol uses LLKHF flags (0x80 for UP)
	isKeyUp := flags&LLKHF_UP != 0

	// Track key state to prevent duplicate key down events
	if isKeyUp {
		delete(vi.pressedKeys, linuxCode)
	} else {
		// If key is already pressed, don't inject another key down event
		if vi.pressedKeys[linuxCode] {
			return
		}
		vi.pressedKeys[linuxCode] = true
	}

	value := int32(1)
	if isKeyUp {
		value = 0
	}

	writeEvent(vi.kbdFile, EV_KEY, linuxCode, value)
	writeEvent(vi.kbdFile, EV_SYN, SYN_REPORT, 0)
}

// ReleaseAllKeys releases all currently pressed keys.
// This should be called when disconnecting to prevent stuck keys.
func (vi *VirtualInput) ReleaseAllKeys() {
	for linuxCode := range vi.pressedKeys {
		writeEvent(vi.kbdFile, EV_KEY, linuxCode, 0)
		delete(vi.pressedKeys, linuxCode)
	}
	writeEvent(vi.kbdFile, EV_SYN, SYN_REPORT, 0)
}

func (vi *VirtualInput) InjectMouse(x, y, wheelDelta, flags int32) {
	absX := int(x) * vi.screenW / 65536
	absY := int(y) * vi.screenH / 65536

	moved := false

	if vi.absAvailable {
		writeEvent(vi.mouseFile, EV_ABS, ABS_X, int32(absX))
		writeEvent(vi.mouseFile, EV_ABS, ABS_Y, int32(absY))
		moved = true
	} else {
		deltaX := absX - vi.absX
		deltaY := absY - vi.absY

		if deltaX != 0 {
			writeEvent(vi.mouseFile, EV_REL, REL_X, int32(deltaX))
			moved = true
		}
		if deltaY != 0 {
			writeEvent(vi.mouseFile, EV_REL, REL_Y, int32(deltaY))
			moved = true
		}
	}
	vi.absX = absX
	vi.absY = absY

	changed := vi.updateButtons(flags, wheelDelta)

	if wheelDelta != 0 {
		linuxWheel := wheelDelta / 120
		if linuxWheel == 0 {
			linuxWheel = 1
			if wheelDelta < 0 {
				linuxWheel = -1
			}
		}
		writeEvent(vi.mouseFile, EV_REL, REL_WHEEL, linuxWheel)
	}

	if changed || wheelDelta != 0 || moved {
		writeEvent(vi.mouseFile, EV_SYN, SYN_REPORT, 0)
	}
}

func (vi *VirtualInput) updateButtons(flags, wheelDelta int32) bool {
	// Windows MWB sends WM_* message types, not bit flags
	// Check exact message types to determine button state
	isLDown := flags == WM_LBUTTONDOWN
	isLUp := flags == WM_LBUTTONUP
	isRDown := flags == WM_RBUTTONDOWN
	isRUp := flags == WM_RBUTTONUP
	isMDown := flags == WM_MBUTTONDOWN
	isMUp := flags == WM_MBUTTONUP
	// For X buttons, the button number is passed in wheelDelta field
	// Per Windows MWB protocol: WheelDelta = 1 for XBUTTON1, 2 for XBUTTON2
	isXDown := flags == WM_XBUTTONDOWN
	isXUp := flags == WM_XBUTTONUP
	// Extract X button number from wheelDelta: 1 = XBUTTON1, 2 = XBUTTON2
	xButtonNum := uint16(wheelDelta)
	isXButton1 := isXDown && xButtonNum == 1
	isXButton2 := isXDown && xButtonNum == 2
	isXButton1Up := isXUp && xButtonNum == 1
	isXButton2Up := isXUp && xButtonNum == 2

	changed := false

	// Handle LEFT button - always inject to prevent stuck buttons
	if isLDown {
		if !vi.btnLeft {
			vi.btnLeft = true
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_LEFT, 1)
		changed = true
	} else if isLUp {
		if vi.btnLeft {
			vi.btnLeft = false
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_LEFT, 0)
		changed = true
	}

	// Handle RIGHT button - always inject to prevent stuck buttons
	if isRDown {
		if !vi.btnRight {
			vi.btnRight = true
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_RIGHT, 1)
		changed = true
	} else if isRUp {
		if vi.btnRight {
			vi.btnRight = false
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_RIGHT, 0)
		changed = true
	}

	// Handle MIDDLE button - always inject to prevent stuck buttons
	if isMDown {
		if !vi.btnMiddle {
			vi.btnMiddle = true
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_MIDDLE, 1)
		changed = true
	} else if isMUp {
		if vi.btnMiddle {
			vi.btnMiddle = false
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_MIDDLE, 0)
		changed = true
	}

	// Handle XBUTTON1 (Side/Back - button 4) - always inject
	if isXButton1 {
		if !vi.btnSide {
			vi.btnSide = true
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_SIDE, 1)
		changed = true
	} else if isXButton1Up {
		if vi.btnSide {
			vi.btnSide = false
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_SIDE, 0)
		changed = true
	}

	// Handle XBUTTON2 (Extra/Forward - button 5) - always inject
	if isXButton2 {
		if !vi.btnExtra {
			vi.btnExtra = true
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_EXTRA, 1)
		changed = true
	} else if isXButton2Up {
		if vi.btnExtra {
			vi.btnExtra = false
		}
		writeEvent(vi.mouseFile, EV_KEY, BTN_EXTRA, 0)
		changed = true
	}

	return changed
}

func (vi *VirtualInput) Close() {
	// Release all pressed keys before closing to prevent stuck keys
	vi.ReleaseAllKeys()

	if vi.mouseFile != nil {
		ioctl(vi.mouseFile.Fd(), UI_DEV_DESTROY, 0)
		vi.mouseFile.Close()
	}
	if vi.kbdFile != nil {
		ioctl(vi.kbdFile.Fd(), UI_DEV_DESTROY, 0)
		vi.kbdFile.Close()
	}
}

func ioctl(fd uintptr, request uintptr, val uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, val)
	if errno != 0 {
		return errno
	}
	return nil
}

func ioctlPtr(fd uintptr, request uintptr, ptr unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, uintptr(ptr))
	if errno != 0 {
		return errno
	}
	return nil
}

func writeEvent(f *os.File, evType uint16, code uint16, value int32) {
	buf := make([]byte, inputEventSize)
	binary.LittleEndian.PutUint64(buf[0:8], 0)
	binary.LittleEndian.PutUint64(buf[8:16], 0)
	binary.LittleEndian.PutUint16(buf[16:18], evType)
	binary.LittleEndian.PutUint16(buf[18:20], code)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(value))
	f.Write(buf)
}
