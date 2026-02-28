package e2e

import (
	"sync"
)

type MockClipboard struct {
	mu          sync.Mutex
	Text        string
	StopCalled  bool
	WatchCalled bool
	OnChange    func(content string)
}

func NewMockClipboard() *MockClipboard {
	return &MockClipboard{}
}

func (m *MockClipboard) GetText() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Text, nil
}

func (m *MockClipboard) SetText(text string) error {
	m.mu.Lock()
	m.Text = text
	m.mu.Unlock()
	if m.OnChange != nil {
		m.OnChange(text)
	}
	return nil
}

func (m *MockClipboard) Watch() {
	m.mu.Lock()
	m.WatchCalled = true
	m.mu.Unlock()
}

func (m *MockClipboard) Stop() {
	m.mu.Lock()
	m.StopCalled = true
	m.mu.Unlock()
}
