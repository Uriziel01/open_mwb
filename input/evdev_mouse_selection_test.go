//go:build linux
// +build linux

package input

import "testing"

func TestRemoteMouseActivationAction_WaitsForIntentionalInput(t *testing.T) {
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}

	tests := []InputEvent{
		{Type: EV_ABS, Code: ABS_X, Value: 123},
		{Type: EV_REL, Code: REL_WHEEL, Value: 1},
		{Type: EV_REL, Code: REL_X, Value: 0},
		{Type: EV_KEY, Code: BTN_LEFT, Value: 0}, // release only should not activate
	}

	for _, ev := range tests {
		if got := remoteMouseActivationAction(nil, mouseA, ev); got != mouseActionNone {
			t.Fatalf("event %+v triggered action %v, want %v", ev, got, mouseActionNone)
		}
	}
}

func TestRemoteMouseActivationAction_ActivatesOnRelMoveOrButtonPress(t *testing.T) {
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}

	moveEvent := InputEvent{Type: EV_REL, Code: REL_X, Value: 5}
	if got := remoteMouseActivationAction(nil, mouseA, moveEvent); got != mouseActionActivate {
		t.Fatalf("REL move action = %v, want %v", got, mouseActionActivate)
	}

	buttonEvent := InputEvent{Type: EV_KEY, Code: BTN_RIGHT, Value: 1}
	if got := remoteMouseActivationAction(nil, mouseA, buttonEvent); got != mouseActionActivate {
		t.Fatalf("button press action = %v, want %v", got, mouseActionActivate)
	}
}

func TestRemoteMouseActivationAction_RebindsWhenDifferentMouseBecomesActive(t *testing.T) {
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}
	mouseB := &DeviceHandle{Info: DeviceInfo{Name: "mouseB"}}

	rebindEvent := InputEvent{Type: EV_REL, Code: REL_Y, Value: -3}
	if got := remoteMouseActivationAction(mouseA, mouseB, rebindEvent); got != mouseActionRebind {
		t.Fatalf("action = %v, want %v", got, mouseActionRebind)
	}

	keepEvent := InputEvent{Type: EV_REL, Code: REL_X, Value: 2}
	if got := remoteMouseActivationAction(mouseA, mouseA, keepEvent); got != mouseActionNone {
		t.Fatalf("same-device action = %v, want %v", got, mouseActionNone)
	}
}
