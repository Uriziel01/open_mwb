package clipboard

import (
	"bytes"
	"log"
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
	return &Clipboard{
		stopCh: make(chan struct{}),
	}
}

func (c *Clipboard) GetText() (string, error) {
	cmd := exec.Command("wl-paste", "--no-newline", "-t", "text/plain")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return out.String(), nil
}

func (c *Clipboard) SetText(text string) error {
	cmd := exec.Command("wl-copy", "--", text)
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

func (c *Clipboard) Stop() {
	close(c.stopCh)
}
