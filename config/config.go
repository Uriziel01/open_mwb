package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Config holds the runtime configuration for MWB Linux.
type Config struct {
	// Network
	SecurityKey      string `json:"key"`
	RemoteAddress    string `json:"remote"`
	ListenPort       int    `json:"port"`
	MachineID        uint32 `json:"id"`
	RemoteMachineID  uint32 `json:"remote_id"`
	MachineName      string `json:"name"`

	// Screen
	ScreenWidth    int    `json:"width"`
	ScreenHeight   int    `json:"height"`

	// Topology: which edge connects to the remote machine
	Edge           string `json:"edge"`

	// Input device paths (auto-detected if empty)
	MouseDevice    string `json:"mouse"`
	KeyboardDevice string `json:"keyboard"`

	// Mode
	Mode           string `json:"mode"`

	// Debug
	Debug          bool   `json:"debug"`

	// CLI-only, not in JSON
	ListDevices    bool   `json:"-"`
	ConfigFile     string `json:"-"`
	ShowVersion    bool   `json:"-"`
}

// defaultConfigPaths returns paths to search for config.json, in priority order.
func defaultConfigPaths() []string {
	paths := []string{}

	// 1. Next to the binary
	exe, err := os.Executable()
	if err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exe), "config.json"))
	}

	// 2. Current working directory
	paths = append(paths, "config.json")

	// 3. Parent directory (for running from open_mwb/ subdir)
	paths = append(paths, "../config.json")

	return paths
}

// loadFromJSON loads config from a JSON file. Returns true if a file was found and loaded.
func (c *Config) loadFromJSON() bool {
	searchPaths := []string{}
	if c.ConfigFile != "" {
		searchPaths = []string{c.ConfigFile}
	} else {
		searchPaths = defaultConfigPaths()
	}

	for _, path := range searchPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if err := json.Unmarshal(data, c); err != nil {
			log.Printf("Warning: failed to parse %s: %v", path, err)
			continue
		}

		absPath, _ := filepath.Abs(path)
		log.Printf("Loaded config from %s", absPath)
		return true
	}

	return false
}

// Parse loads config from JSON first, then applies CLI flag overrides.
func Parse() *Config {
	c := &Config{
		// Defaults
		ListenPort:   15100,
		MachineID:    1,
		ScreenWidth:  1920,
		ScreenHeight: 1080,
		Edge:         "right",
		Mode:         "client",
	}

	// Define flags (all optional since JSON provides defaults)
	var (
		flagKey      = flag.String("key", "", "MWB security key (must match Windows)")
		flagRemote   = flag.String("remote", "", "Remote Windows machine IP/hostname")
		flagPort     = flag.Int("port", 0, "TCP listen port")
		flagID       = flag.Uint("id", 0, "This machine's MWB ID (from MachinePool)")
		flagRemoteID = flag.Uint("remote-id", 0, "Remote machine's MWB ID (from MachinePool)")
		flagWidth    = flag.Int("width", 0, "Local screen width in pixels")
		flagHeight   = flag.Int("height", 0, "Local screen height in pixels")
		flagEdge     = flag.String("edge", "", "Edge where remote machine is (left/right/top/bottom)")
		flagMouse    = flag.String("mouse", "", "Mouse /dev/input/event path (auto-detect if empty)")
		flagKeyboard = flag.String("keyboard", "", "Keyboard /dev/input/event path (auto-detect if empty)")
		flagMode     = flag.String("mode", "", "Run mode: client, server, or tui")
		flagConfig   = flag.String("config", "", "Path to config.json (auto-detected if empty)")
		flagDebug    = flag.Bool("debug", false, "Enable debug packet logging")
	)
	flag.BoolVar(&c.ListDevices, "list-devices", false, "List all /dev/input devices and exit")
	flag.BoolVar(&c.ShowVersion, "version", false, "Print version and exit")

	flag.Parse()

	// Load config file path from flag first
	c.ConfigFile = *flagConfig

	// Load JSON config (sets base values)
	c.loadFromJSON()

	// Apply CLI flag overrides (only if explicitly set)
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "key":
			c.SecurityKey = *flagKey
		case "remote":
			c.RemoteAddress = *flagRemote
		case "port":
			c.ListenPort = *flagPort
		case "id":
			c.MachineID = uint32(*flagID)
		case "remote-id":
			c.RemoteMachineID = uint32(*flagRemoteID)
		case "width":
			c.ScreenWidth = *flagWidth
		case "height":
			c.ScreenHeight = *flagHeight
		case "edge":
			c.Edge = *flagEdge
		case "mouse":
			c.MouseDevice = *flagMouse
		case "keyboard":
			c.KeyboardDevice = *flagKeyboard
		case "mode":
			c.Mode = *flagMode
		case "debug":
			c.Debug = *flagDebug
		}
	})

	// Auto-detect machine name from hostname if not set
	if c.MachineName == "" {
		if h, err := os.Hostname(); err == nil {
			c.MachineName = h
		}
	}

	return c
}

// Validate checks that required fields are present.
func (c *Config) Validate() error {
	if c.ListDevices {
		return nil
	}

	if c.SecurityKey == "" {
		return fmt.Errorf("security key is required (set in config.json or use --key)")
	}

	if (c.Mode == "client" || c.Mode == "tui") && c.RemoteAddress == "" {
		return fmt.Errorf("remote address is required in %s mode (set in config.json or use --remote)", c.Mode)
	}

	validEdges := map[string]bool{"left": true, "right": true, "top": true, "bottom": true}
	if !validEdges[c.Edge] {
		return fmt.Errorf("edge must be one of: left, right, top, bottom")
	}

	if c.MachineID == 0 {
		return fmt.Errorf("machine id is required (set in config.json or use --id)")
	}

	return nil
}

// PrintUsage prints usage instructions.
func PrintUsage() {
	fmt.Fprintf(os.Stderr, `Mouse Without Borders - Linux POC
Usage:
  open-mwb [options]

Config is loaded from config.json (auto-detected next to binary, in cwd,
or in parent dir). CLI flags override config.json values.

config.json example:
  {
    "key": "YourSecurityKey",
    "remote": "192.168.1.100",
    "edge": "right",
    "mode": "tui",
    "id": 2
  }

Options:
`)
	flag.PrintDefaults()
}
