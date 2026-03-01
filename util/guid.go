package util

import (
	"strings"

	"github.com/google/uuid"
)

// MWBTextSeparator is the standard GUID separator used by Windows MWB for text clipboard format
const MWBTextSeparator = "{4CFF57F7-BEDD-43d5-AE8F-27A61E886F2F}"

// GenerateGUID generates a new random GUID in the format {XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX}
func GenerateGUID() string {
	id := uuid.New()
	return "{" + id.String() + "}"
}

// GenerateMWBClipboardFormat creates a properly formatted clipboard string for Windows MWB
// Format: "TXT<payload>{GUID}"
func GenerateMWBClipboardFormat(payload string) string {
	// This is the standard MWB separator GUID
	// While we can generate random GUIDs, Windows MWB expects this specific one for parsing
	return "TXT" + payload + MWBTextSeparator
}

// GenerateMWBClipboardFormatWithGUID creates a clipboard string with a custom GUID
// This can be used if MWB protocol supports variable GUIDs in the future
func GenerateMWBClipboardFormatWithGUID(payload string, guid string) string {
	// Ensure GUID is wrapped in braces
	if !strings.HasPrefix(guid, "{") {
		guid = "{" + guid
	}
	if !strings.HasSuffix(guid, "}") {
		guid = guid + "}"
	}
	return "TXT" + payload + guid
}

// ParseMWBClipboardFormat extracts the payload from an MWB-formatted clipboard string
// It removes the "TXT" prefix and the GUID suffix
func ParseMWBClipboardFormat(formatted string) (string, error) {
	// Simple parsing for test
	parsed := formatted

	// Strip format prefix (TXT, UNI, RTF, HTM)
	if len(parsed) >= 3 {
		prefix := parsed[:3]
		if prefix == "TXT" || prefix == "UNI" || prefix == "RTF" || prefix == "HTM" {
			parsed = parsed[3:]
		}
	}

	// Strip the suffix and anything after
	if idx := strings.Index(parsed, MWBTextSeparator); idx != -1 {
		parsed = parsed[:idx]
	}

	// Sometimes null bytes are appended at the end
	parsed = strings.TrimRight(parsed, "\x00")

	return parsed, nil
}
