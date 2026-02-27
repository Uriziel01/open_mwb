//go:build linux
// +build linux

package input

// VirtualInputInterface defines the interface for virtual input devices
type VirtualInputInterface interface {
	InjectMouse(x, y, wheelDelta, flags int32)
	InjectKeyboard(vk int32, flags int32)
	Close()
}
