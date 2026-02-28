package e2e

import (
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// AutoItClient interacts with the custom AutoIt Network Control Server running on the Windows VM.
type AutoItClient struct {
	Address string
}

func NewAutoItClient(ip string) *AutoItClient {
	return &AutoItClient{
		Address: fmt.Sprintf("%s:15102", ip),
	}
}

func (c *AutoItClient) sendCommand(cmd string) (string, error) {
	conn, err := net.DialTimeout("tcp", c.Address, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to connect to AutoIt server at %s: %w", c.Address, err)
	}
	defer conn.Close()

	// Set a deadline for the entire operation
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	_, err = conn.Write([]byte(cmd))
	if err != nil {
		return "", fmt.Errorf("failed to send command %q: %w", cmd, err)
	}

	// Close write half if supported, to signal EOF to the server if it expects it.
	// But the AutoIt server might just read the string and close or respond and close.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.CloseWrite()
	}

	// Read response (if any)
	response, err := io.ReadAll(conn)
	if err != nil {
		return "", fmt.Errorf("failed to read response for command %q: %w", cmd, err)
	}

	return strings.TrimSpace(string(response)), nil
}

// PressKey sends keystrokes using ControlSend.
func (c *AutoItClient) PressKey(key string) error {
	_, err := c.sendCommand(fmt.Sprintf("pressKey:%s", key))
	return err
}

// MoveMouse moves the mouse cursor to absolute coordinates (x,y).
func (c *AutoItClient) MoveMouse(x, y int) error {
	_, err := c.sendCommand(fmt.Sprintf("moveMouse:%d,%d", x, y))
	return err
}

// SetClipboard sets the system clipboard to the provided content.
func (c *AutoItClient) SetClipboard(text string) error {
	_, err := c.sendCommand(fmt.Sprintf("setClipboard:%s", text))
	return err
}

// GetMousePos returns the current absolute mouse position as "x,y".
func (c *AutoItClient) GetMousePos() (string, error) {
	return c.sendCommand("getMousePos")
}

// GetClipboardContent returns the current system clipboard content.
func (c *AutoItClient) GetClipboardContent() (string, error) {
	return c.sendCommand("getClipboardContent")
}

// GetInputContent returns the current text in the GUI input field and empties it.
func (c *AutoItClient) GetInputContent() (string, error) {
	return c.sendCommand("getInputContent")
}

// ImgToClipboard puts a sample image into the clipboard on the Windows side.
func (c *AutoItClient) ImgToClipboard() error {
	_, err := c.sendCommand("imgToClipboard")
	return err
}

// GetDesktopSize returns the screen dimensions of the Windows VM as "width,height".
func (c *AutoItClient) GetDesktopSize() (string, error) {
	return c.sendCommand("getDesktopSize")
}
