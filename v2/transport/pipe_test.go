package transport

import (
	"log"
	"net"
	"net/url"
	"testing"
)

func TestPipe(t *testing.T) {
	listener, addrURL, err := NewListener()
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	urlParsed, err := url.Parse(addrURL)
	if err != nil {
		t.Fatalf("failed to parse address URL: %v", err)
	}

	var family string
	if urlParsed.Scheme == "unix" {
		family = "unix"
	} else if urlParsed.Scheme == "dns" {
		family = "tcp"
	} else {
		t.Fatalf("unsupported address URL scheme: %s", urlParsed.Scheme)
	}

	go func() {
		conn, err := net.Dial(family, listener.Addr().String())
		if err != nil {
			log.Panicf("failed to dial listener: %v", err)
		}
		if _, err := conn.Write([]byte("test")); err != nil {
			log.Panicf("failed to write to listener: %v", err)
		}
		conn.Close()
	}()

	accepted, err := listener.Accept()
	if err != nil {
		t.Fatalf("failed to accept listener: %v", err)
	}
	var buf [1024]byte
	n, err := accepted.Read(buf[:])
	if err != nil {
		t.Fatalf("failed to read from listener: %v", err)
	}
	if string(buf[:n]) != "test" {
		t.Fatalf("expected test, got %s", string(buf[:n]))
	}
	accepted.Write([]byte("test"))
	accepted.Close()

}
