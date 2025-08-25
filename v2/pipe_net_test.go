package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"
)

type dummyInfraServer struct {
	protobuf.UnimplementedPluginServer
}

func (s *dummyInfraServer) GetPluginInfo(ctx context.Context, req *emptypb.Empty) (*protobuf.Info, error) {
	return &protobuf.Info{
		Version: "test",
	}, nil
}

func (s *dummyInfraServer) GetServerVersion(ctx context.Context, req *emptypb.Empty) (*protobuf.ServerVersionInfo, error) {
	return &protobuf.ServerVersionInfo{
		Version:   "test",
		Commit:    "test",
		BuildDate: time.Now().Format(time.RFC3339),
	}, nil
}

func TestGrpcPipeNet(t *testing.T) {
	serverPub, serverPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tlsClient, err := NewEphemeralTLSClient()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := newTCPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverCsrBytes, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{
			CommonName: BuildPluginTLSName(PurposePluginRPC, "test"),
		},
		DNSNames: []string{
			BuildPluginTLSName(PurposePluginRPC, "test"),
		},
		PublicKey: serverPub,
	}, serverPriv)
	serverCsr, err := x509.ParseCertificateRequest(serverCsrBytes)
	if err != nil {
		t.Fatal(err)
	}
	serverCertBytes, err := tlsClient.SignPluginCSR("test", serverCsr)
	if err != nil {
		t.Fatal(err)
	}
	clientPipe := NewGrpcPipeTLS(fmt.Sprintf("[::1]:%d", listener.Addr().(*net.TCPAddr).Port), tlsClient.ClientTLSConfig("test"))

	serverTLSConfig := tlsClient.ServerTLSConfig()
	serverTLSConfig.Certificates = []tls.Certificate{
		{
			Certificate: [][]byte{serverCertBytes},
			PrivateKey:  serverPriv,
		},
	}

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLSConfig)))
	protobuf.RegisterPluginServer(server, &dummyInfraServer{})
	go server.Serve(listener)
	defer server.GracefulStop()

	conn, err := clientPipe.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	infraClient := protobuf.NewPluginClient(conn)
	version, err := infraClient.GetPluginInfo(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if version.Version != "test" {
		t.Fatal("expected test, got ", version.Version)
	}

	defer conn.Close()
}
