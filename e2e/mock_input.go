package e2e

import "sync"

type MockInput struct {
	mu           sync.Mutex
	MouseEvents  []MouseEvent
	KeyEvents    []KeyEvent
	Closed       bool
}

type MouseEvent struct {
	X, Y, WheelDelta, Flags int32
}

type KeyEvent struct {
	VK, Flags int32
}

func NewMockInput() *MockInput {
	return &MockInput{}
}

func (m *MockInput) InjectMouse(x, y, wheelDelta, flags int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.MouseEvents = append(m.MouseEvents, MouseEvent{X: x, Y: y, WheelDelta: wheelDelta, Flags: flags})
}

func (m *MockInput) InjectKeyboard(vk int32, flags int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.KeyEvents = append(m.KeyEvents, KeyEvent{VK: vk, Flags: flags})
}

func (m *MockInput) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
}

func (m *MockInput) GetMouseEvents() []MouseEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]MouseEvent(nil), m.MouseEvents...)
}

func (m *MockInput) GetKeyEvents() []KeyEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]KeyEvent(nil), m.KeyEvents...)
}
