//go:build !unix

package plugin

import "net"

func NewListener() (net.Listener, error) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		return nil, err
	}
	return listener, nil
}
