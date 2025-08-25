//go:build unix

package transport

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func NewListener() (net.Listener, string, error) {
	tmpDir, err := os.MkdirTemp("", "gotify-plugin-*")
	if err != nil {
		return nil, "", err
	}
	if err := os.Chmod(tmpDir, 0700); err != nil {
		return nil, "", err
	}
	pipePath := filepath.Join(tmpDir, "plugin.sock")
	listener, err := net.Listen("unix", pipePath)
	if err != nil {
		return nil, "", err
	}
	return listener, fmt.Sprintf("unix://%s", pipePath), nil
}
