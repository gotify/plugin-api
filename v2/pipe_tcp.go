package plugin

import (
	"fmt"
	"net"
)

func NewTCPListener() (net.Listener, string, error) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		return nil, "", err
	}
	return listener, fmt.Sprintf("dns:///%s", listener.Addr().String()), nil
}
