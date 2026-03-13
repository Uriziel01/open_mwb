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

func TestDecideMouseActivationLocked_RequiresTwoMoveEvents(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}

	move := InputEvent{Type: EV_REL, Code: REL_X, Value: 4}

	e.mu.Lock()
	first := e.decideMouseActivationLocked(mouseA, move)
	second := e.decideMouseActivationLocked(mouseA, move)
	e.mu.Unlock()

	if first != mouseActionNone {
		t.Fatalf("first move action = %v, want %v", first, mouseActionNone)
	}
	if second != mouseActionActivate {
		t.Fatalf("second move action = %v, want %v", second, mouseActionActivate)
	}
}

func TestDecideMouseActivationLocked_ButtonPressIsImmediate(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}

	press := InputEvent{Type: EV_KEY, Code: BTN_LEFT, Value: 1}

	e.mu.Lock()
	got := e.decideMouseActivationLocked(mouseA, press)
	e.mu.Unlock()

	if got != mouseActionActivate {
		t.Fatalf("button press action = %v, want %v", got, mouseActionActivate)
	}
}

func TestDecideMouseActivationLocked_IgnoresTinyJitter(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}

	smallMove := InputEvent{Type: EV_REL, Code: REL_X, Value: 1}

	e.mu.Lock()
	first := e.decideMouseActivationLocked(mouseA, smallMove)
	second := e.decideMouseActivationLocked(mouseA, smallMove)
	third := e.decideMouseActivationLocked(mouseA, smallMove)
	fourth := e.decideMouseActivationLocked(mouseA, smallMove)
	e.mu.Unlock()

	if first != mouseActionNone || second != mouseActionNone || third != mouseActionNone {
		t.Fatalf("tiny jitter activated too early: [%v %v %v]", first, second, third)
	}
	if fourth != mouseActionActivate {
		t.Fatalf("fourth tiny move action = %v, want %v", fourth, mouseActionActivate)
	}
}

func TestDecideMouseActivationLocked_IgnoresAbsNoise(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	mouseA := &DeviceHandle{Info: DeviceInfo{Name: "mouseA"}}

	abs := InputEvent{Type: EV_ABS, Code: ABS_X, Value: 300}

	e.mu.Lock()
	got := e.decideMouseActivationLocked(mouseA, abs)
	e.mu.Unlock()

	if got != mouseActionNone {
		t.Fatalf("ABS event action = %v, want %v", got, mouseActionNone)
	}
}

func TestMouseSourceForKeyboardButtonLocked_MapsByPhysicalDevice(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	mouse := &DeviceHandle{
		Info: DeviceInfo{
			Name: "Logitech G603",
			Path: "/dev/input/event21",
			Phys: "usb-0000:00:14.0-2/input1",
		},
	}
	kbd := &DeviceHandle{
		Info: DeviceInfo{
			Name: "Logitech USB Receiver",
			Path: "/dev/input/event3",
			Phys: "usb-0000:00:14.0-2/input0",
		},
	}
	e.mouseDevs = []*DeviceHandle{mouse}

	e.mu.Lock()
	got := e.mouseSourceForKeyboardButtonLocked(kbd)
	e.mu.Unlock()

	if got != mouse {
		t.Fatalf("mapped mouse = %v, want %v", got, mouse)
	}
}

func TestMouseSourceForKeyboardButtonLocked_SkipsWhenKeyboardPathIsMousePath(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	mouse := &DeviceHandle{
		Info: DeviceInfo{
			Name: "Logitech G603",
			Path: "/dev/input/event3",
		},
	}
	kbd := &DeviceHandle{
		Info: DeviceInfo{
			Name: "Logitech G603",
			Path: "/dev/input/event3",
		},
	}
	e.mouseDevs = []*DeviceHandle{mouse}

	e.mu.Lock()
	got := e.mouseSourceForKeyboardButtonLocked(kbd)
	e.mu.Unlock()

	if got != nil {
		t.Fatalf("expected nil mapping for shared path, got %v", got)
	}
}
