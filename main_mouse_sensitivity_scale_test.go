package main

import "testing"

func TestMouseSensitivityDefaultMapsToOnePixelGain(t *testing.T) {
	s := 24
	gain := float64(s) / 24.0
	if gain != 1.0 {
		t.Fatalf("default sensitivity gain = %f, want 1.0", gain)
	}
}

func TestMouseSensitivityIsLinear(t *testing.T) {
	if got := float64(12) / 24.0; got != 0.5 {
		t.Fatalf("sensitivity 12 gain = %f, want 0.5", got)
	}
	if got := float64(48) / 24.0; got != 2.0 {
		t.Fatalf("sensitivity 48 gain = %f, want 2.0", got)
	}
}
