//go:build linux
// +build linux

package input

import "testing"

func TestHandleRemoteMouseEvent_IgnoresButtonRepeat(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)
	calls := 0
	e.OnButtonEvent = func(code uint16, pressed bool) {
		calls++
	}

	e.handleRemoteMouseEvent(InputEvent{
		Type:  EV_KEY,
		Code:  BTN_LEFT,
		Value: 2, // repeat/hold
	})

	if calls != 0 {
		t.Fatalf("expected repeat event to be ignored, got %d callbacks", calls)
	}
}

func TestHandleRemoteMouseEvent_ForwardsPressAndRelease(t *testing.T) {
	e := NewEvdevCapture(1920, 1080)

	type evt struct {
		code    uint16
		pressed bool
	}
	var got []evt
	e.OnButtonEvent = func(code uint16, pressed bool) {
		got = append(got, evt{code: code, pressed: pressed})
	}

	e.handleRemoteMouseEvent(InputEvent{Type: EV_KEY, Code: BTN_LEFT, Value: 1})
	e.handleRemoteMouseEvent(InputEvent{Type: EV_KEY, Code: BTN_LEFT, Value: 0})

	if len(got) != 2 {
		t.Fatalf("expected 2 callbacks, got %d", len(got))
	}
	if got[0] != (evt{code: BTN_LEFT, pressed: true}) {
		t.Fatalf("unexpected press callback: %+v", got[0])
	}
	if got[1] != (evt{code: BTN_LEFT, pressed: false}) {
		t.Fatalf("unexpected release callback: %+v", got[1])
	}
}
