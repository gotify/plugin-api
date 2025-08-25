package transport

import (
	"net"
	"testing"
)

func TestPipe(t *testing.T) {
	listener, _, err := NewListener()
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	go func() {
		conn, err := net.Dial("unix", listener.Addr().String())
		if err != nil {
			panic(err)
		}
		conn.Write([]byte("test"))
		conn.Close()
	}()

	accepted, err := listener.Accept()
	if err != nil {
		panic(err)
	}
	var buf [1024]byte
	n, err := accepted.Read(buf[:])
	if err != nil {
		panic(err)
	}
	if string(buf[:n]) != "test" {
		panic("expected test, got " + string(buf[:n]))
	}
	accepted.Write([]byte("test"))
	accepted.Close()

}
