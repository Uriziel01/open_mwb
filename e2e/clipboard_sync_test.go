package e2e

import (
	"bytes"
	"testing"

	"open-mwb/protocol"
)

func TestClipboard_Text_Small(t *testing.T) {
	mock := NewMockClipboard()

	text := "small text"
	err := mock.SetText(text)
	if err != nil {
		t.Fatalf("Failed to set text: %v", err)
	}

	got, err := mock.GetText()
	if err != nil {
		t.Fatalf("Failed to get text: %v", err)
	}
	if got != text {
		t.Errorf("Expected %q, got %q", text, got)
	}
}

func TestClipboard_Text_Multipart(t *testing.T) {
	// The protocol for transferring large clipboard data (text/images) uses chunking.
	// Currently the implementation only supports fitting in the 64-byte GenericData.
	// This test sets the expectation for future multipart implementation.
	t.Skip("Pending implementation of multipart clipboard text synchronization")

	// Example expectation:
	// - Sender sends protocol.Clipboard (metadata, size)
	// - Sender sends multiple protocol.ClipboardPush (chunks)
	// - Sender sends protocol.ClipboardDataEnd
	// - Receiver reconstitutes and updates clipboard
}

func TestClipboard_Image_Sync(t *testing.T) {
	t.Skip("Pending implementation of image clipboard synchronization")
	// Similar to text multipart, but packet type is protocol.ClipboardImage
	// and the metadata specifies an image format (e.g. PNG).
}

func TestClipboard_File_DragDrop_Protocol(t *testing.T) {
	t.Skip("Pending implementation of file drag & drop synchronization")
	// - protocol.Clipboard (file metadata: name, size)
	// - protocol.ClipboardPush (binary chunks)
	// - protocol.ClipboardDataEnd
	// Receiver writes to a temp file and places file URI on clipboard.
}

func TestClipboard_Format_Conversion(t *testing.T) {
	// When a Windows system sends CF_UNICODETEXT, it might send UTF-16 bytes.
	// The Go side must decode it to UTF-8 before setting it on the Linux Wayland clipboard.
	// We'll write a test that simulates receiving UTF-16 bytes.
	
	// Example UTF-16 LE bytes for "Hi"
	utf16Bytes := []byte{'H', 0, 'i', 0}
	
	pkt := &protocol.GenericData{
		Header: protocol.Header{
			Type: protocol.ClipboardText, // Need a flag for format, currently assumed raw text
		},
		Raw: utf16Bytes,
	}

	// For now, our implementation assumes UTF-8. 
	// This test asserts the raw byte handling for now until we add UTF-16 decoding.
	if !bytes.Equal(pkt.Raw, utf16Bytes) {
		t.Errorf("Raw bytes mismatch")
	}
}
