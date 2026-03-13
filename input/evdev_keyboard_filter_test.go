//go:build linux
// +build linux

package input

import "testing"

func TestMouseButtonsAreNotKeyboardKeys(t *testing.T) {
	for _, code := range []uint16{BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, BTN_SIDE, BTN_EXTRA} {
		if !isMouseButtonCode(code) {
			t.Fatalf("expected code %d to be recognized as mouse button", code)
		}
	}
}

func TestTypicalKeyboardCodesAreNotMouseButtons(t *testing.T) {
	for _, code := range []uint16{
		30,  // KEY_A
		57,  // KEY_SPACE
		59,  // KEY_F1
		60,  // KEY_F2
		125, // KEY_LEFTMETA
		126, // KEY_RIGHTMETA
	} {
		if isMouseButtonCode(code) {
			t.Fatalf("expected code %d to NOT be recognized as mouse button", code)
		}
	}
}
