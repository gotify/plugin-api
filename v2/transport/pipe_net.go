package transport

import (
	"context"
	"crypto/tls"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func newTCPListener() (net.Listener, error) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		return nil, err
	}
	return listener, nil
}

type GrpcPipeTLS struct {
	address   string
	tlsConfig *tls.Config
}

func NewGrpcPipeTLS(address string, config *tls.Config) *GrpcPipeTLS {
	return &GrpcPipeTLS{
		address:   address,
		tlsConfig: config,
	}
}

func (p *GrpcPipeTLS) Dial(ctx context.Context) (*grpc.ClientConn, error) {
	return grpc.NewClient(p.address, grpc.WithTransportCredentials(credentials.NewTLS(p.tlsConfig)))
}
