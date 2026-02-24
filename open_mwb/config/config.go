package config

import (
	"flag"
	"fmt"
	"os"
)

// Config holds the runtime configuration for MWB Linux.
type Config struct {
	// Network
	SecurityKey    string
	RemoteAddress  string // IP/hostname of the Windows MWB machine
	ListenPort     int
	MachineID      uint32

	// Screen
	ScreenWidth    int
	ScreenHeight   int

	// Topology: which edge connects to the remote machine
	// "right" = remote machine is to the right of this screen
	// "left"  = remote machine is to the left
	Edge           string

	// Input device paths (auto-detected if empty)
	MouseDevice    string
	KeyboardDevice string

	// Mode
	Mode           string // "client" or "server"

	// Debug
	ListDevices    bool
}

// Parse parses CLI flags into a Config.
func Parse() *Config {
	c := &Config{}

	flag.StringVar(&c.SecurityKey, "key", "", "MWB security key (must match Windows)")
	flag.StringVar(&c.RemoteAddress, "remote", "", "Remote Windows machine IP/hostname")
	flag.IntVar(&c.ListenPort, "port", 15100, "TCP listen port")
	var machineIDTmp uint
	flag.UintVar(&machineIDTmp, "id", 1, "Machine ID (1-4)")
	flag.IntVar(&c.ScreenWidth, "width", 1920, "Local screen width in pixels")
	flag.IntVar(&c.ScreenHeight, "height", 1080, "Local screen height in pixels")
	flag.StringVar(&c.Edge, "edge", "right", "Edge where remote machine is (left/right/top/bottom)")
	flag.StringVar(&c.MouseDevice, "mouse", "", "Mouse /dev/input/event path (auto-detect if empty)")
	flag.StringVar(&c.KeyboardDevice, "keyboard", "", "Keyboard /dev/input/event path (auto-detect if empty)")
	flag.StringVar(&c.Mode, "mode", "client", "Run mode: 'client' (connect to Windows) or 'server' (listen for Windows)")
	flag.BoolVar(&c.ListDevices, "list-devices", false, "List all /dev/input devices and exit")

	flag.Parse()

	c.MachineID = uint32(machineIDTmp)

	return c
}

// Validate checks that required fields are present.
func (c *Config) Validate() error {
	if c.ListDevices {
		return nil // skip validation for list mode
	}

	if c.SecurityKey == "" {
		return fmt.Errorf("--key is required (MWB security key)")
	}

	if c.Mode == "client" && c.RemoteAddress == "" {
		return fmt.Errorf("--remote is required in client mode")
	}

	validEdges := map[string]bool{"left": true, "right": true, "top": true, "bottom": true}
	if !validEdges[c.Edge] {
		return fmt.Errorf("--edge must be one of: left, right, top, bottom")
	}

	if c.MachineID < 1 || c.MachineID > 4 {
		return fmt.Errorf("--id must be between 1 and 4")
	}

	return nil
}

// PrintUsage prints usage instructions.
func PrintUsage() {
	fmt.Fprintf(os.Stderr, `Mouse Without Borders - Linux POC
Usage:
  mwb-linux --key <security-key> --remote <windows-ip> [options]

Examples:
  # Connect to Windows machine on the right side
  sudo mwb-linux --key "MySecretKey" --remote 192.168.1.100 --edge right

  # Listen for Windows connection, screen on the left
  sudo mwb-linux --key "MySecretKey" --mode server --edge left

  # Auto-detect devices, custom resolution
  sudo mwb-linux --key "MyKey" --remote 192.168.1.50 --width 2560 --height 1440

  # List available input devices
  mwb-linux --list-devices

Options:
`)
	flag.PrintDefaults()
}

// UintVar is a workaround because flag doesn't have UintVar for uint32.
func UintVar(p *uint, name string, value uint, usage string) {
	flag.UintVar(p, name, value, usage)
}
