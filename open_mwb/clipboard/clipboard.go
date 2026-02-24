package clipboard

import (
	"bytes"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Clipboard handles Wayland clipboard sync using wl-copy/wl-paste.
// It watches for local clipboard changes and provides methods to
// set/get clipboard content for MWB synchronization.

type Clipboard struct {
	mu          sync.Mutex
	lastContent string
	stopCh      chan struct{}

	// Callback when local clipboard changes (to send to remote)
	OnChange func(content string)
}

func New() *Clipboard {
	return &Clipboard{
		stopCh: make(chan struct{}),
	}
}

// GetText reads the current text clipboard using wl-paste.
func (c *Clipboard) GetText() (string, error) {
	cmd := exec.Command("wl-paste", "--no-newline", "-t", "text/plain")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

// SetText sets the clipboard text using wl-copy.
func (c *Clipboard) SetText(text string) error {
	cmd := exec.Command("wl-copy", "--", text)
	err := cmd.Run()
	if err != nil {
		return err
	}

	// Update our known content to prevent re-sending
	c.mu.Lock()
	c.lastContent = text
	c.mu.Unlock()

	log.Printf("[clipboard] Set text (%d chars)", len(text))
	return nil
}

// Watch polls the clipboard for changes and calls OnChange.
// This is a simple polling approach since Wayland doesn't allow
// passive clipboard monitoring from non-focused applications.
// Call this in a goroutine.
func (c *Clipboard) Watch() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			content, err := c.GetText()
			if err != nil {
				continue
			}

			c.mu.Lock()
			changed := content != c.lastContent && strings.TrimSpace(content) != ""
			if changed {
				c.lastContent = content
			}
			c.mu.Unlock()

			if changed && c.OnChange != nil {
				c.OnChange(content)
			}
		}
	}
}

// Stop stops the clipboard watcher.
func (c *Clipboard) Stop() {
	close(c.stopCh)
}
