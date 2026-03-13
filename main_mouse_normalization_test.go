package main

import "testing"

func TestPixelToAbsolute_ClampsRange(t *testing.T) {
	if got := pixelToAbsolute(-10, 1920); got != 0 {
		t.Fatalf("pixelToAbsolute(-10, 1920) = %d, want 0", got)
	}
	if got := pixelToAbsolute(2500, 1920); got != 65535 {
		t.Fatalf("pixelToAbsolute(2500, 1920) = %d, want 65535", got)
	}
}

func TestPixelToAbsolute_UsesAxisSize(t *testing.T) {
	// Same 100px motion should produce a smaller absolute delta on a wider axis.
	x0 := pixelToAbsolute(1920, 3840)
	x1 := pixelToAbsolute(2020, 3840)
	y0 := pixelToAbsolute(540, 1080)
	y1 := pixelToAbsolute(640, 1080)

	dx := x1 - x0
	dy := y1 - y0
	if dx >= dy {
		t.Fatalf("expected x delta < y delta for same pixel move on wider axis, got dx=%d dy=%d", dx, dy)
	}
}
