//go:build linux
// +build linux

package input

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// UInput creates virtual input devices via /dev/uinput to inject
// keyboard and mouse events received from the remote Windows machine.

// uinput ioctl constants
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

// uinput_setup structure (matches kernel's struct uinput_setup)
type uinputSetup struct {
	ID   inputID
	Name [80]byte
	_    uint32 // ff_effects_max
}

type inputID struct {
	Bustype uint16
	Vendor  uint16
	Product uint16
	Version uint16
}

// uinput_abs_setup
type uinputAbsSetup struct {
	Code    uint16
	_       [2]byte // padding
	Minimum int32
	Maximum int32
	Fuzz    int32
	Flat    int32
	Res     int32
}

// VirtualInput manages virtual keyboard and mouse devices.
type VirtualInput struct {
	mouseFile *os.File
	kbdFile   *os.File
	screenW   int
	screenH   int

	// Track absolute position for the virtual mouse
	absX int
	absY int
}

// NewVirtualInput creates virtual keyboard and mouse via /dev/uinput.
func NewVirtualInput(screenW, screenH int) (*VirtualInput, error) {
	vi := &VirtualInput{
		screenW: screenW,
		screenH: screenH,
		absX:    0,
		absY:    0,
	}

	var err error

	// ---- Create virtual mouse ----
	vi.mouseFile, err = os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open /dev/uinput for mouse: %w", err)
	}

	if err := vi.setupMouse(); err != nil {
		vi.mouseFile.Close()
		return nil, err
	}

	// ---- Create virtual keyboard ----
	vi.kbdFile, err = os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		vi.mouseFile.Close()
		return nil, fmt.Errorf("failed to open /dev/uinput for keyboard: %w", err)
	}

	if err := vi.setupKeyboard(); err != nil {
		vi.mouseFile.Close()
		vi.kbdFile.Close()
		return nil, err
	}

	log.Printf("[uinput] Virtual mouse and keyboard created (screen %dx%d)", screenW, screenH)
	return vi, nil
}

func ioctl(fd uintptr, request uintptr, val uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, request, val)
	if errno != 0 {
		return errno
	}
	return nil
}

func (vi *VirtualInput) setupMouse() error {
	fd := vi.mouseFile.Fd()

	// Enable event types: EV_KEY (buttons), EV_REL (relative movement), EV_ABS (absolute), EV_SYN
	for _, evType := range []uintptr{EV_SYN, EV_KEY, EV_REL, EV_ABS} {
		if err := ioctl(fd, UI_SET_EVBIT, evType); err != nil {
			return fmt.Errorf("UI_SET_EVBIT %d: %w", evType, err)
		}
	}

	// Enable mouse buttons
	for _, btn := range []uintptr{BTN_LEFT, BTN_RIGHT, BTN_MIDDLE} {
		if err := ioctl(fd, UI_SET_KEYBIT, btn); err != nil {
			return fmt.Errorf("UI_SET_KEYBIT btn %d: %w", btn, err)
		}
	}

	// Enable relative axes
	for _, rel := range []uintptr{REL_X, REL_Y, REL_WHEEL} {
		if err := ioctl(fd, UI_SET_RELBIT, rel); err != nil {
			return fmt.Errorf("UI_SET_RELBIT %d: %w", rel, err)
		}
	}

	// Enable absolute axes for Windows-style absolute positioning
	for _, abs := range []uintptr{0x00, 0x01} { // ABS_X, ABS_Y
		if err := ioctl(fd, UI_SET_ABSBIT, abs); err != nil {
			return fmt.Errorf("UI_SET_ABSBIT %d: %w", abs, err)
		}
	}

	// Setup absolute axis ranges
	// ABS_X
	absSetupX := uinputAbsSetup{
		Code:    0x00, // ABS_X
		Minimum: 0,
		Maximum: int32(vi.screenW - 1),
	}
	if err := ioctlPtr(fd, UI_ABS_SETUP, unsafe.Pointer(&absSetupX)); err != nil {
		return fmt.Errorf("UI_ABS_SETUP X: %w", err)
	}

	// ABS_Y
	absSetupY := uinputAbsSetup{
		Code:    0x01, // ABS_Y
		Minimum: 0,
		Maximum: int32(vi.screenH - 1),
	}
	if err := ioctlPtr(fd, UI_ABS_SETUP, unsafe.Pointer(&absSetupY)); err != nil {
		return fmt.Errorf("UI_ABS_SETUP Y: %w", err)
	}

	// Device setup
	setup := uinputSetup{
		ID: inputID{
			Bustype: 0x03, // BUS_USB
			Vendor:  0x1234,
			Product: 0x5678,
			Version: 1,
		},
	}
	copy(setup.Name[:], "MWB Virtual Mouse")
	if err := ioctlPtr(fd, UI_DEV_SETUP, unsafe.Pointer(&setup)); err != nil {
		return fmt.Errorf("UI_DEV_SETUP mouse: %w", err)
	}

	if err := ioctl(fd, UI_DEV_CREATE, 0); err != nil {
		return fmt.Errorf("UI_DEV_CREATE mouse: %w", err)
	}

	return nil
}

func (vi *VirtualInput) setupKeyboard() error {
	fd := vi.kbdFile.Fd()

	// Enable EV_KEY and EV_SYN
	for _, evType := range []uintptr{EV_SYN, EV_KEY} {
		if err := ioctl(fd, UI_SET_EVBIT, evType); err != nil {
			return fmt.Errorf("UI_SET_EVBIT %d: %w", evType, err)
		}
	}

	// Enable all standard keys (1-248)
	for key := uintptr(1); key <= 248; key++ {
		ioctl(fd, UI_SET_KEYBIT, key)
	}

	// Device setup
	setup := uinputSetup{
		ID: inputID{
			Bustype: 0x03, // BUS_USB
			Vendor:  0x1234,
			Product: 0x5679,
			Version: 1,
		},
	}
	copy(setup.Name[:], "MWB Virtual Keyboard")
	if err := ioctlPtr(fd, UI_DEV_SETUP, unsafe.Pointer(&setup)); err != nil {
		return fmt.Errorf("UI_DEV_SETUP keyboard: %w", err)
	}

	if err := ioctl(fd, UI_DEV_CREATE, 0); err != nil {
		return fmt.Errorf("UI_DEV_CREATE keyboard: %w", err)
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

// writeEvent writes a single input_event to a uinput device file.
func writeEvent(f *os.File, evType uint16, code uint16, value int32) error {
	buf := make([]byte, inputEventSize)
	// timeval can be zero, kernel fills it
	binary.LittleEndian.PutUint64(buf[0:8], 0)
	binary.LittleEndian.PutUint64(buf[8:16], 0)
	binary.LittleEndian.PutUint16(buf[16:18], evType)
	binary.LittleEndian.PutUint16(buf[18:20], code)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(value))
	_, err := f.Write(buf)
	return err
}

func (vi *VirtualInput) syn(f *os.File) error {
	return writeEvent(f, EV_SYN, 0, 0)
}

// InjectKeyboard injects a keyboard event from a remote Windows machine.
// vk is the Windows Virtual Key code, flags contains KEYEVENTF_* constants.
func (vi *VirtualInput) InjectKeyboard(vk int32, flags int32) {
	linuxCode, ok := VKToLinux[vk]
	if !ok {
		log.Printf("[uinput] Unknown VK code: 0x%X", vk)
		return
	}

	value := int32(1) // key down
	if flags&WinKeyEventFKeyUp != 0 {
		value = 0 // key up
	}

	writeEvent(vi.kbdFile, EV_KEY, linuxCode, value)
	vi.syn(vi.kbdFile)
}

// InjectMouse injects a mouse event from a remote Windows machine.
// This handles absolute positioning, relative movement, buttons, and wheel.
func (vi *VirtualInput) InjectMouse(x, y, wheelDelta, flags int32) {
	if flags&WinMouseEventFAbsolute != 0 {
		// Absolute positioning: Windows sends 0-65535, scale to screen
		absX := int(x) * vi.screenW / 65536
		absY := int(y) * vi.screenH / 65536

		writeEvent(vi.mouseFile, EV_ABS, 0x00, int32(absX)) // ABS_X
		writeEvent(vi.mouseFile, EV_ABS, 0x01, int32(absY)) // ABS_Y
		vi.absX = absX
		vi.absY = absY
	} else if flags&WinMouseEventFMove != 0 {
		// Relative movement
		if x != 0 {
			writeEvent(vi.mouseFile, EV_REL, REL_X, x)
		}
		if y != 0 {
			writeEvent(vi.mouseFile, EV_REL, REL_Y, y)
		}
	}

	// Button events
	if flags&WinMouseEventFLeftDown != 0 {
		writeEvent(vi.mouseFile, EV_KEY, BTN_LEFT, 1)
	}
	if flags&WinMouseEventFLeftUp != 0 {
		writeEvent(vi.mouseFile, EV_KEY, BTN_LEFT, 0)
	}
	if flags&WinMouseEventFRightDown != 0 {
		writeEvent(vi.mouseFile, EV_KEY, BTN_RIGHT, 1)
	}
	if flags&WinMouseEventFRightUp != 0 {
		writeEvent(vi.mouseFile, EV_KEY, BTN_RIGHT, 0)
	}
	if flags&WinMouseEventFMiddleDown != 0 {
		writeEvent(vi.mouseFile, EV_KEY, BTN_MIDDLE, 1)
	}
	if flags&WinMouseEventFMiddleUp != 0 {
		writeEvent(vi.mouseFile, EV_KEY, BTN_MIDDLE, 0)
	}

	// Wheel
	if flags&WinMouseEventFWheel != 0 && wheelDelta != 0 {
		// Windows sends 120/-120 per notch, Linux expects 1/-1
		linuxWheel := wheelDelta / 120
		if linuxWheel == 0 {
			if wheelDelta > 0 {
				linuxWheel = 1
			} else {
				linuxWheel = -1
			}
		}
		writeEvent(vi.mouseFile, EV_REL, REL_WHEEL, linuxWheel)
	}

	vi.syn(vi.mouseFile)
}

// Close destroys the virtual devices.
func (vi *VirtualInput) Close() {
	if vi.mouseFile != nil {
		ioctl(vi.mouseFile.Fd(), UI_DEV_DESTROY, 0)
		vi.mouseFile.Close()
	}
	if vi.kbdFile != nil {
		ioctl(vi.kbdFile.Fd(), UI_DEV_DESTROY, 0)
		vi.kbdFile.Close()
	}
	log.Println("[uinput] Virtual devices destroyed")
}
