//go:build unix

package plugin

import (
	"net"
	"os"
	"path/filepath"
)

func NewListener() (net.Listener, error) {
	tmpDir, err := os.MkdirTemp("", "gotify-plugin-*")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(tmpDir, 0700); err != nil {
		return nil, err
	}
	pipePath := filepath.Join(tmpDir, "plugin.sock")
	return net.Listen("unix", pipePath)
}
