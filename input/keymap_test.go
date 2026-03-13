package input

import "testing"

func TestLinuxPrintScreenAliasesMapToVKSnapshot(t *testing.T) {
	tests := []uint16{
		99,  // KEY_SYSRQ
		210, // KEY_PRINT
	}

	for _, code := range tests {
		vk, ok := LinuxToVK[code]
		if !ok {
			t.Fatalf("expected Linux key code %d to be mapped", code)
		}
		if vk != 0x2C {
			t.Fatalf("Linux key code %d mapped to VK 0x%02X, want 0x2C (VK_SNAPSHOT)", code, vk)
		}
	}
}
