//go:build !unix

package transport

import (
	"net"
)

func NewListener() (net.Listener, string, error) {
	return NewTCPListener()
}
