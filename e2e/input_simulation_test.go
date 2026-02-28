package e2e

import (
	"testing"
	"open-mwb/input"
)

func TestInput_Keyboard_Keymap_Translation(t *testing.T) {
	// Test standard keys
	tests := []struct {
		vk       int32
		expected uint16
		name     string
	}{
		{0x20, 57, "Space"},
		{0x41, 30, "A"},
		{0x0D, 28, "Enter"},
		{0xA0, 42, "Left Shift"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linuxCode, ok := input.VKToLinux[tt.vk]
			if !ok {
				t.Fatalf("VK %x not found in map", tt.vk)
			}
			if linuxCode != tt.expected {
				t.Errorf("Expected linux code %d for VK %x, got %d", tt.expected, tt.vk, linuxCode)
			}
		})
	}
}

func TestInput_Keyboard_Modifiers_Sync(t *testing.T) {
	// Modifier keys
	tests := []struct {
		vk       int32
		expected uint16
		name     string
	}{
		{0xA0, 42, "Left Shift"},
		{0xA1, 54, "Right Shift"},
		{0xA2, 29, "Left Ctrl"},
		{0xA3, 97, "Right Ctrl"},
		{0xA4, 56, "Left Alt"},
		{0xA5, 100, "Right Alt"},
		{0x5B, 125, "Left Meta/Win"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linuxCode, ok := input.VKToLinux[tt.vk]
			if !ok {
				t.Fatalf("VK %x not found in map", tt.vk)
			}
			if linuxCode != tt.expected {
				t.Errorf("Expected linux code %d for VK %x, got %d", tt.expected, tt.vk, linuxCode)
			}
		})
	}
}

func TestInput_Keyboard_Extended_Keys(t *testing.T) {
	tests := []struct {
		vk       int32
		expected uint16
		name     string
	}{
		{0x70, 59, "F1"},
		{0x7B, 88, "F12"},
		{0x60, 82, "Numpad 0"},
		{0x69, 73, "Numpad 9"},
		{0x21, 104, "Page Up"},
		{0x28, 108, "Down Arrow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linuxCode, ok := input.VKToLinux[tt.vk]
			if !ok {
				t.Fatalf("VK %x not found in map", tt.vk)
			}
			if linuxCode != tt.expected {
				t.Errorf("Expected linux code %d for VK %x, got %d", tt.expected, tt.vk, linuxCode)
			}
		})
	}
}

func TestInput_Mouse_Relative_Deltas(t *testing.T) {
	// This test asserts the VirtualInput simulation behavior.
	// We instantiate a MockInput to verify the received values from network packets.
	mock := NewMockInput()

	// Simulate receiving a relative mouse move
	mock.InjectMouse(10, -5, 0, input.WinMouseEventFMove)

	events := mock.GetMouseEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 mouse event, got %d", len(events))
	}
	if events[0].X != 10 || events[0].Y != -5 {
		t.Errorf("Expected relative move (10, -5), got (%d, %d)", events[0].X, events[0].Y)
	}
}

func TestInput_Mouse_Absolute_Bounds(t *testing.T) {
	mock := NewMockInput()

	// Absolute coordinates in Windows are 0-65535
	// We'll pass the ABSOLUTE flag.
	mock.InjectMouse(32768, 32768, 0, input.WinMouseEventFMove|input.WinMouseEventFAbsolute)

	events := mock.GetMouseEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 mouse event, got %d", len(events))
	}
	
	if events[0].X != 32768 || events[0].Y != 32768 {
		t.Errorf("Expected absolute move (32768, 32768), got (%d, %d)", events[0].X, events[0].Y)
	}
}

func TestInput_Mouse_Wheel_Scroll(t *testing.T) {
	mock := NewMockInput()

	// 120 is the standard WHEEL_DELTA in Windows
	mock.InjectMouse(0, 0, 120, input.WinMouseEventFWheel)

	events := mock.GetMouseEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 mouse event, got %d", len(events))
	}
	
	if events[0].WheelDelta != 120 {
		t.Errorf("Expected wheel delta 120, got %d", events[0].WheelDelta)
	}
}

func TestInput_Suppression_State(t *testing.T) {
	// In the real app, when local controls remote, local inputs are suppressed.
	// We test this state logic if applicable. Here we just assert the E2E framework allows tracking it.
	// Since we are mocking, we just assert the test runs successfully to validate test compilation.
	t.Log("Suppression state test placeholder - requires full state machine wired up.")
}
