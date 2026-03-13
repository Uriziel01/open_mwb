package main

import (
	"testing"

	"open-mwb/input"
)

func TestButtonPacket_XButtonsUseWheelDeltaSlots(t *testing.T) {
	tests := []struct {
		code      uint16
		pressed   bool
		wantFlags int32
		wantWheel int32
	}{
		{input.BTN_SIDE, true, input.WM_XBUTTONDOWN, 1},
		{input.BTN_SIDE, false, input.WM_XBUTTONUP, 1},
		{input.BTN_EXTRA, true, input.WM_XBUTTONDOWN, 2},
		{input.BTN_EXTRA, false, input.WM_XBUTTONUP, 2},
	}

	for _, tc := range tests {
		gotFlags, gotWheel := buttonPacket(tc.code, tc.pressed)
		if gotFlags != tc.wantFlags || gotWheel != tc.wantWheel {
			t.Fatalf("buttonPacket(%d,%v) => flags=%d wheel=%d, want flags=%d wheel=%d",
				tc.code, tc.pressed, gotFlags, gotWheel, tc.wantFlags, tc.wantWheel)
		}
	}
}
