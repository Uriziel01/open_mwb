package clipboard

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Clipboard struct {
	mu          sync.Mutex
	lastContent string
	stopCh      chan struct{}
	OnChange    func(content string)
}

func New() *Clipboard {
	c := &Clipboard{
		stopCh: make(chan struct{}),
	}
	
	// Try to get initial clipboard content
	if content, err := c.GetText(); err == nil {
		c.lastContent = content
		log.Printf("[clipboard] Initial content: %d chars", len(content))
	} else {
		log.Printf("[clipboard] Warning: Could not read initial clipboard: %v", err)
	}
	
	return c
}

func (c *Clipboard) GetText() (string, error) {
	// Ensure we have proper Wayland environment
	env := os.Environ()
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	
	// If running as root (e.g., with sudo), try to detect user's Wayland session
	if os.Getuid() == 0 {
		// Try to find user's Wayland display from common locations
		if waylandDisplay == "" {
			// Common pattern: wayland-0, wayland-1
			for i := 0; i < 10; i++ {
				display := fmt.Sprintf("wayland-%d", i)
				socketPath := fmt.Sprintf("/run/user/1000/%s", display)
				if _, err := os.Stat(socketPath); err == nil {
					waylandDisplay = display
					xdgRuntimeDir = "/run/user/1000"
					log.Printf("[clipboard] Auto-detected Wayland display: %s", display)
					break
				}
			}
		}
		
		// Update environment for the command
		newEnv := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, "WAYLAND_DISPLAY=") && !strings.HasPrefix(e, "XDG_RUNTIME_DIR=") {
				newEnv = append(newEnv, e)
			}
		}
		if waylandDisplay != "" {
			newEnv = append(newEnv, fmt.Sprintf("WAYLAND_DISPLAY=%s", waylandDisplay))
		}
		if xdgRuntimeDir != "" {
			newEnv = append(newEnv, fmt.Sprintf("XDG_RUNTIME_DIR=%s", xdgRuntimeDir))
		}
		env = newEnv
	}
	
	if waylandDisplay == "" {
		return "", fmt.Errorf("WAYLAND_DISPLAY not set (try running with sudo -E to preserve environment)")
	}

	// Try text/plain first with captured stderr
	cmd := exec.Command("wl-paste", "--no-newline", "-t", "text/plain")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Env = env
	err := cmd.Run()
	if err == nil {
		return out.String(), nil
	}
	
	stderrStr := stderr.String()
	
	// Check if clipboard is empty (special case) - handle various messages
	lowerStderr := strings.ToLower(stderrStr)
	if strings.Contains(lowerStderr, "nothing is copied") || 
	   strings.Contains(lowerStderr, "empty") || 
	   strings.Contains(lowerStderr, "no selection") ||
	   strings.Contains(lowerStderr, "clipboard") {
		// Clipboard is empty, return empty string without error
		return "", nil
	}
	
	if stderrStr != "" {
		log.Printf("[clipboard] wl-paste stderr (text/plain): %s", stderrStr)
	}
	
	// Fallback to text/plain;charset=utf-8
	cmd = exec.Command("wl-paste", "--no-newline", "-t", "text/plain;charset=utf-8")
	out.Reset()
	stderr.Reset()
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Env = env
	err = cmd.Run()
	if err == nil {
		return out.String(), nil
	}
	
	if stderrStr := stderr.String(); stderrStr != "" {
		log.Printf("[clipboard] wl-paste stderr (utf-8): %s", stderrStr)
	}
	
	// Fallback without type specification
	cmd = exec.Command("wl-paste", "--no-newline")
	out.Reset()
	stderr.Reset()
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	cmd.Env = env
	err = cmd.Run()
	if err == nil {
		return out.String(), nil
	}
	
	return "", fmt.Errorf("wl-paste failed: %v (stderr: %s)", err, stderr.String())
}

func (c *Clipboard) SetText(text string) error {
	// Ensure we have proper Wayland environment (same logic as GetText)
	env := os.Environ()
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	
	// If running as root (e.g., with sudo), try to detect user's Wayland session
	if os.Getuid() == 0 {
		// Try to find user's Wayland display from common locations
		if waylandDisplay == "" {
			// Common pattern: wayland-0, wayland-1
			for i := 0; i < 10; i++ {
				display := fmt.Sprintf("wayland-%d", i)
				socketPath := fmt.Sprintf("/run/user/1000/%s", display)
				if _, err := os.Stat(socketPath); err == nil {
					waylandDisplay = display
					xdgRuntimeDir = "/run/user/1000"
					log.Printf("[clipboard] Auto-detected Wayland display for set: %s", display)
					break
				}
			}
		}
		
		// Update environment for the command
		newEnv := make([]string, 0, len(env))
		for _, e := range env {
			if !strings.HasPrefix(e, "WAYLAND_DISPLAY=") && !strings.HasPrefix(e, "XDG_RUNTIME_DIR=") {
				newEnv = append(newEnv, e)
			}
		}
		if waylandDisplay != "" {
			newEnv = append(newEnv, fmt.Sprintf("WAYLAND_DISPLAY=%s", waylandDisplay))
		}
		if xdgRuntimeDir != "" {
			newEnv = append(newEnv, fmt.Sprintf("XDG_RUNTIME_DIR=%s", xdgRuntimeDir))
		}
		env = newEnv
	}
	
	if waylandDisplay == "" {
		return fmt.Errorf("WAYLAND_DISPLAY not set (try running with sudo -E to preserve environment)")
	}

	cmd := exec.Command("wl-copy", "--", text)
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return err
	}

	c.mu.Lock()
	c.lastContent = text
	c.mu.Unlock()

	log.Printf("[clipboard] Set %d chars", len(text))
	return nil
}

func (c *Clipboard) Watch() {
	ticker := time.NewTicker(2000 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			content, err := c.GetText()
			if err != nil {
				// Only log if not a "normal" empty clipboard error
				errStr := err.Error()
				if !strings.Contains(errStr, "empty") && !strings.Contains(errStr, "No selection") {
					log.Printf("[clipboard] wl-paste error: %v", err)
				}
				continue
			}

			c.mu.Lock()
			contentTrimmed := strings.TrimSpace(content)
			lastTrimmed := strings.TrimSpace(c.lastContent)
			changed := contentTrimmed != lastTrimmed && contentTrimmed != ""
			if changed {
				log.Printf("[clipboard] Detected change: prev=%d chars, new=%d chars", len(c.lastContent), len(content))
				c.lastContent = content
			}
			c.mu.Unlock()

			if changed && c.OnChange != nil {
				c.OnChange(content)
			}
		}
	}
}

func (c *Clipboard) Stop() {
	close(c.stopCh)
}
