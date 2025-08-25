//go:build !unix

package plugin

import (
	"net"
)

func NewListener() (net.Listener, string, error) {
	return NewTCPListener()
}
