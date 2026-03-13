//go:build windows
// +build windows

package input

// VirtualInput stub for Windows
type VirtualInput struct{}

const (
	BTN_LEFT   = 0x110
	BTN_RIGHT  = 0x111
	BTN_MIDDLE = 0x112
)

func NewVirtualInput(screenW, screenH int) (*VirtualInput, error) {
	return &VirtualInput{}, nil
}

func (vi *VirtualInput) Close() {}

func (vi *VirtualInput) InjectMouse(x, y, wheel, flags int32) {}

func (vi *VirtualInput) InjectKeyboard(vk, flags int32) {}

func (vi *VirtualInput) ReleaseAllKeys() {}

// EvdevCapture stub for Windows
type EvdevCapture struct {
	IsRemote          bool
	OnLocalCursorMove func(x, y int32)
	OnEdgeHit         func()
	OnReturn          func()
	OnMouseEvent      func(dx, dy, wheelDelta int)
	OnButtonEvent     func(code uint16, pressed bool)
	OnKeyEvent        func(code uint16, pressed bool)
}

func NewEvdevCapture(screenW, screenH int, edge string) *EvdevCapture {
	return &EvdevCapture{}
}

func (e *EvdevCapture) Open(mousePath, kbdPath string) error {
	return nil
}

func (e *EvdevCapture) DiscoverAndOpen() error {
	return nil
}

func (e *EvdevCapture) Grab(activeMouse interface{}) error {
	return nil
}

func (e *EvdevCapture) Ungrab() error {
	return nil
}

func (e *EvdevCapture) Close() {}

func (e *EvdevCapture) RunMouseLoop() {}

func (e *EvdevCapture) RunKeyboardLoop() {}

func (e *EvdevCapture) SwitchToLocal() {}

func (e *EvdevCapture) IsRemoteMode() bool {
	return e.IsRemote
}

// Helpers
func FindMouseDevice() (string, error) {
	return "dummy-mouse", nil
}

func FindKeyboardDevice() (string, error) {
	return "dummy-kbd", nil
}

func ListDevices() {
}
