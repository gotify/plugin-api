package transport

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net"
	"testing"
	"time"
)

func TestEphemeralTLSClient(t *testing.T) {
	client, err := NewEphemeralTLSClient()
	if err != nil {
		t.Fatal(err)
	}

	pluginTlsName := BuildPluginTLSName(PurposePluginRPC, "test")
	_, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverCSRBytes, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: pluginTlsName,
		},
		DNSNames: []string{
			BuildPluginTLSName(PurposePluginRPC, "test"),
		},
	}, serverPriv)
	if err != nil {
		t.Fatal(err)
	}
	serverCSR, err := x509.ParseCertificateRequest(serverCSRBytes)
	if err != nil {
		t.Fatal(err)
	}
	serverCert, err := client.SignPluginCSR("test", serverCSR)
	if err != nil {
		t.Fatal(err)
	}

	s, c := net.Pipe()
	defer s.Close()
	defer c.Close()
	go func() {
		serverTLSConfig := client.ServerTLSConfig()
		serverTLSConfig.Certificates = []tls.Certificate{
			{
				Certificate: [][]byte{serverCert},
				PrivateKey:  serverPriv,
			},
		}
		tlsServer := tls.Server(s, serverTLSConfig)
		_, err = tlsServer.Write([]byte("hello"))
		if err != nil {
			panic(err)
		}
	}()

	tlsClient := tls.Client(c, client.ClientTLSConfig("test"))
	tlsClient.SetDeadline(time.Now().Add(time.Second * 1))
	buf := make([]byte, 1024)
	n, err := tlsClient.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatal("expected hello, got ", string(buf[:n]))
	}

}
