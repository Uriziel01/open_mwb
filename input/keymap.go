package input

// Windows Virtual Key codes -> Linux input-event-codes.h KEY_* values.
// This maps the most common keys used in everyday input.
// Reference: https://learn.microsoft.com/en-us/windows/win32/inputdev/virtual-key-codes
// Reference: /usr/include/linux/input-event-codes.h

// VKToLinux maps Windows Virtual Key codes to Linux KEY_* event codes.
var VKToLinux = map[int32]uint16{
	// Mouse buttons (handled separately, but defined for completeness)
	0x01: 0x110, // VK_LBUTTON -> BTN_LEFT
	0x02: 0x111, // VK_RBUTTON -> BTN_RIGHT
	0x04: 0x112, // VK_MBUTTON -> BTN_MIDDLE
	0x05: 0x113, // VK_XBUTTON1 -> BTN_SIDE (Mouse button 4/Back)
	0x06: 0x114, // VK_XBUTTON2 -> BTN_EXTRA (Mouse button 5/Forward)

	// Common control keys
	0x08: 14,  // VK_BACK -> KEY_BACKSPACE
	0x09: 15,  // VK_TAB -> KEY_TAB
	0x0D: 28,  // VK_RETURN -> KEY_ENTER
	0x10: 42,  // VK_SHIFT -> KEY_LEFTSHIFT
	0x11: 29,  // VK_CONTROL -> KEY_LEFTCTRL
	0x12: 56,  // VK_MENU (Alt) -> KEY_LEFTALT
	0x13: 119, // VK_PAUSE -> KEY_PAUSE
	0x14: 58,  // VK_CAPITAL -> KEY_CAPSLOCK
	0x1B: 1,   // VK_ESCAPE -> KEY_ESC
	0x20: 57,  // VK_SPACE -> KEY_SPACE

	// Navigation
	0x21: 104, // VK_PRIOR (PgUp) -> KEY_PAGEUP
	0x22: 109, // VK_NEXT (PgDn) -> KEY_PAGEDOWN
	0x23: 107, // VK_END -> KEY_END
	0x24: 102, // VK_HOME -> KEY_HOME
	0x25: 105, // VK_LEFT -> KEY_LEFT
	0x26: 103, // VK_UP -> KEY_UP
	0x27: 106, // VK_RIGHT -> KEY_RIGHT
	0x28: 108, // VK_DOWN -> KEY_DOWN
	0x2C: 210, // VK_SNAPSHOT (PrtSc) -> KEY_PRINT
	0x2D: 110, // VK_INSERT -> KEY_INSERT
	0x2E: 111, // VK_DELETE -> KEY_DELETE

	// Numbers 0-9
	0x30: 11, // VK_0 -> KEY_0
	0x31: 2,  // VK_1 -> KEY_1
	0x32: 3,  // VK_2 -> KEY_2
	0x33: 4,  // VK_3 -> KEY_3
	0x34: 5,  // VK_4 -> KEY_4
	0x35: 6,  // VK_5 -> KEY_5
	0x36: 7,  // VK_6 -> KEY_6
	0x37: 8,  // VK_7 -> KEY_7
	0x38: 9,  // VK_8 -> KEY_8
	0x39: 10, // VK_9 -> KEY_9

	// Letters A-Z
	0x41: 30, // VK_A -> KEY_A
	0x42: 48, // VK_B -> KEY_B
	0x43: 46, // VK_C -> KEY_C
	0x44: 32, // VK_D -> KEY_D
	0x45: 18, // VK_E -> KEY_E
	0x46: 33, // VK_F -> KEY_F
	0x47: 34, // VK_G -> KEY_G
	0x48: 35, // VK_H -> KEY_H
	0x49: 23, // VK_I -> KEY_I
	0x4A: 36, // VK_J -> KEY_J
	0x4B: 37, // VK_K -> KEY_K
	0x4C: 38, // VK_L -> KEY_L
	0x4D: 50, // VK_M -> KEY_M
	0x4E: 49, // VK_N -> KEY_N
	0x4F: 24, // VK_O -> KEY_O
	0x50: 25, // VK_P -> KEY_P
	0x51: 16, // VK_Q -> KEY_Q
	0x52: 19, // VK_R -> KEY_R
	0x53: 31, // VK_S -> KEY_S
	0x54: 20, // VK_T -> KEY_T
	0x55: 22, // VK_U -> KEY_U
	0x56: 47, // VK_V -> KEY_V
	0x57: 17, // VK_W -> KEY_W
	0x58: 45, // VK_X -> KEY_X
	0x59: 21, // VK_Y -> KEY_Y
	0x5A: 44, // VK_Z -> KEY_Z

	// Windows / Super keys
	0x5B: 125, // VK_LWIN -> KEY_LEFTMETA
	0x5C: 126, // VK_RWIN -> KEY_RIGHTMETA

	// Numpad
	0x60: 82, // VK_NUMPAD0 -> KEY_KP0
	0x61: 79, // VK_NUMPAD1 -> KEY_KP1
	0x62: 80, // VK_NUMPAD2 -> KEY_KP2
	0x63: 81, // VK_NUMPAD3 -> KEY_KP3
	0x64: 75, // VK_NUMPAD4 -> KEY_KP4
	0x65: 76, // VK_NUMPAD5 -> KEY_KP5
	0x66: 77, // VK_NUMPAD6 -> KEY_KP6
	0x67: 71, // VK_NUMPAD7 -> KEY_KP7
	0x68: 72, // VK_NUMPAD8 -> KEY_KP8
	0x69: 73, // VK_NUMPAD9 -> KEY_KP9
	0x6A: 55, // VK_MULTIPLY -> KEY_KPASTERISK
	0x6B: 78, // VK_ADD -> KEY_KPPLUS
	0x6D: 74, // VK_SUBTRACT -> KEY_KPMINUS
	0x6E: 83, // VK_DECIMAL -> KEY_KPDOT
	0x6F: 98, // VK_DIVIDE -> KEY_KPSLASH
	0x90: 69, // VK_NUMLOCK -> KEY_NUMLOCK

	// Function keys F1-F12
	0x70: 59, // VK_F1 -> KEY_F1
	0x71: 60, // VK_F2 -> KEY_F2
	0x72: 61, // VK_F3 -> KEY_F3
	0x73: 62, // VK_F4 -> KEY_F4
	0x74: 63, // VK_F5 -> KEY_F5
	0x75: 64, // VK_F6 -> KEY_F6
	0x76: 65, // VK_F7 -> KEY_F7
	0x77: 66, // VK_F8 -> KEY_F8
	0x78: 67, // VK_F9 -> KEY_F9
	0x79: 68, // VK_F10 -> KEY_F10
	0x7A: 87, // VK_F11 -> KEY_F11
	0x7B: 88, // VK_F12 -> KEY_F12

	// Modifier distinguishers
	0xA0: 42,  // VK_LSHIFT -> KEY_LEFTSHIFT
	0xA1: 54,  // VK_RSHIFT -> KEY_RIGHTSHIFT
	0xA2: 29,  // VK_LCONTROL -> KEY_LEFTCTRL
	0xA3: 97,  // VK_RCONTROL -> KEY_RIGHTCTRL
	0xA4: 56,  // VK_LMENU -> KEY_LEFTALT
	0xA5: 100, // VK_RMENU -> KEY_RIGHTALT

	// Punctuation / OEM keys (US layout)
	0xBA: 39, // VK_OEM_1 (;:) -> KEY_SEMICOLON
	0xBB: 13, // VK_OEM_PLUS (=+) -> KEY_EQUAL
	0xBC: 51, // VK_OEM_COMMA (,<) -> KEY_COMMA
	0xBD: 12, // VK_OEM_MINUS (-_) -> KEY_MINUS
	0xBE: 52, // VK_OEM_PERIOD (.>) -> KEY_DOT
	0xBF: 53, // VK_OEM_2 (/?) -> KEY_SLASH
	0xC0: 41, // VK_OEM_3 (`~) -> KEY_GRAVE
	0xDB: 26, // VK_OEM_4 ([{) -> KEY_LEFTBRACE
	0xDC: 43, // VK_OEM_5 (\|) -> KEY_BACKSLASH
	0xDD: 27, // VK_OEM_6 (]}) -> KEY_RIGHTBRACE
	0xDE: 40, // VK_OEM_7 ('") -> KEY_APOSTROPHE

	// Scroll Lock
	0x91: 70, // VK_SCROLL -> KEY_SCROLLLOCK
}

// LinuxToVK is the reverse mapping for sending local keys to Windows.
var LinuxToVK map[uint16]int32

func init() {
	LinuxToVK = make(map[uint16]int32, len(VKToLinux))
	for vk, linux := range VKToLinux {
		LinuxToVK[linux] = vk
	}

	// Some Linux keyboards emit PrintScreen as KEY_SYSRQ (99)
	// while others emit KEY_PRINT (210). Both should map to VK_SNAPSHOT.
	LinuxToVK[99] = 0x2C
}

// Windows LLKHF (Low-Level Keyboard Hook Flags) - used in MWB network protocol
const (
	LLKHF_EXTENDED = 0x01
	LLKHF_INJECTED = 0x10
	LLKHF_ALTDOWN  = 0x20
	LLKHF_UP       = 0x80
)

// Windows KEYEVENTF flags
const (
	WinKeyEventFExtendedKey = 0x0001
	WinKeyEventFKeyUp       = 0x0002
)

// Windows MOUSEEVENTF flags (for SendInput API)
const (
	WinMouseEventFMove       = 0x0001
	WinMouseEventFLeftDown   = 0x0002
	WinMouseEventFLeftUp     = 0x0004
	WinMouseEventFRightDown  = 0x0008
	WinMouseEventFRightUp    = 0x0010
	WinMouseEventFMiddleDown = 0x0020
	WinMouseEventFMiddleUp   = 0x0040
	WinMouseEventFWheel      = 0x0800
	WinMouseEventFAbsolute   = 0x8000
)

// Windows mouse message flags (wParam) - sent in MouseData.Flags field
const (
	WinMouseMKLButton  = 0x0001 // Left button is down
	WinMouseMKRButton  = 0x0002 // Right button is down
	WinMouseMKMButton  = 0x0010 // Middle button is down
	WinMouseMKXButton1 = 0x0020 // X1 button is down
	WinMouseMKXButton2 = 0x0040 // X2 button is down
)

// Windows Messages (WM_*) for remote mouse injection
const (
	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONDOWN = 0x0201
	WM_LBUTTONUP   = 0x0202
	WM_RBUTTONDOWN = 0x0204
	WM_RBUTTONUP   = 0x0205
	WM_MBUTTONDOWN = 0x0207
	WM_MBUTTONUP   = 0x0208
	WM_XBUTTONDOWN = 0x020B
	WM_XBUTTONUP   = 0x020C
	WM_MOUSEWHEEL  = 0x020A
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
)
