package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/emptypb"
)

type GrpcDialer interface {
	Dial(ctx context.Context) (*grpc.ClientConn, error)
}

type PluginRpc struct {
	infraDialer  GrpcDialer
	pluginServer *grpc.Server
}

func NewPluginRpc(infraDialer GrpcDialer) *PluginRpc {
	infraClient, err := infraDialer.Dial(context.Background())
	if err != nil {
		panic(err)
	}
	infraRpcClient := protobuf.NewInfraClient(infraClient)
	version, err := infraRpcClient.GetServerVersion(context.Background(), &emptypb.Empty{})
	if err != nil {
		panic(err)
	}
	_ = version

	return &PluginRpc{
		infraDialer:  infraDialer,
		pluginServer: grpc.NewServer(),
	}
}

func (h *PluginRpc) Serve(listener net.Listener) error {
	return h.pluginServer.Serve(listener)
}

type ServerVersionInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

type infraServerImpl struct {
	server  *ServerMux
	version ServerVersionInfo
	protobuf.UnimplementedInfraServer
}

func (s *infraServerImpl) GetServerVersion(ctx context.Context, req *emptypb.Empty) (*protobuf.ServerVersionInfo, error) {
	return &protobuf.ServerVersionInfo{
		Version:   s.version.Version,
		Commit:    s.version.Commit,
		BuildDate: s.version.BuildDate,
	}, nil
}

func (s *infraServerImpl) WhoAmI(ctx context.Context, req *emptypb.Empty) (*protobuf.Info, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no peer in context")
	}
	authInfo := peer.AuthInfo.(*infraTlsAuthInfo)
	return s.server.GetPluginInfo(authInfo.moduleName)
}

type PluginConnection struct {
	info *protobuf.Info
	conn *grpc.ClientConn
}

type ServerMux struct {
	version               ServerVersionInfo
	tlsClient             *EphemeralTLSClient
	infraAddr             net.Addr
	infraListener         net.Listener
	infraServer           *grpc.Server
	pluginDNSToModulePath map[string]string
	pluginConnections     map[string]PluginConnection
	protobuf.UnimplementedInfraServer
}

func (s *ServerMux) GetServerVersion(ctx context.Context, req *emptypb.Empty) (*protobuf.ServerVersionInfo, error) {
	return &protobuf.ServerVersionInfo{
		Version:   s.version.Version,
		Commit:    s.version.Commit,
		BuildDate: s.version.BuildDate,
	}, nil
}

func (s *ServerMux) WhoAmI(ctx context.Context, req *emptypb.Empty) (*protobuf.Info, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no peer in context")
	}
	authInfo := peer.AuthInfo.(*infraTlsAuthInfo)
	return s.GetPluginInfo(authInfo.moduleName)
}

type infraTlsCreds struct {
	pluginDNSToModulePath map[string]string
	credentials.TransportCredentials
}

type infraTlsAuthInfo struct {
	moduleName string
	credentials.TLSInfo
}

func (c *infraTlsCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	netConn, authInfo, err := c.TransportCredentials.ServerHandshake(rawConn)
	if err != nil {
		log.Printf("ServerHandshake: error %v", err)
		rawConn.Close()
		return nil, nil, err
	}
	protocolInfo := authInfo.(credentials.TLSInfo)
	serverName := protocolInfo.State.VerifiedChains[0][0].DNSNames[0]
	moduleName, ok := c.pluginDNSToModulePath[serverName]
	if !ok {
		log.Printf("ServerHandshake: unknown server name %s", serverName)
		netConn.Close()
		rawConn.Close()
		return nil, nil, fmt.Errorf("unknown server name %s", serverName)
	}

	return netConn, &infraTlsAuthInfo{
		moduleName: moduleName,
		TLSInfo:    protocolInfo,
	}, nil
}

// NewServerMux creates a server-side mux with an infra server that handles plugin-to-server calls.
func NewServerMux(info ServerVersionInfo) *ServerMux {
	tlsClient, err := NewEphemeralTLSClient()
	if err != nil {
		panic(err)
	}
	_, infraPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	infraCsrBytes, err := x509.CreateCertificateRequest(rand.Reader, new(x509.CertificateRequest), infraPriv)
	if err != nil {
		panic(err)
	}
	infraCsr, err := x509.ParseCertificateRequest(infraCsrBytes)
	if err != nil {
		panic(err)
	}
	if err := infraCsr.CheckSignature(); err != nil {
		panic(err)
	}
	infraCert, err := tlsClient.SignCSR(ServerTLSName, infraCsr)
	if err != nil {
		panic(err)
	}
	infraCertParsed, err := x509.ParseCertificate(infraCert)
	if err != nil {
		panic(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AddCert(tlsClient.caCert)
	infraTlsConfig := &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{infraCert, tlsClient.caCert.Raw},
				PrivateKey:  infraPriv,
				Leaf:        infraCertParsed,
			},
		},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caCertPool,
		ServerName: ServerTLSName,
	}

	pluginDNSToModulePath := make(map[string]string)
	infraServer := grpc.NewServer(grpc.Creds(&infraTlsCreds{
		pluginDNSToModulePath: pluginDNSToModulePath,
		TransportCredentials:  credentials.NewTLS(infraTlsConfig),
	}))

	listener, err := newListener()
	if err != nil {
		panic(err)
	}
	mux := &ServerMux{
		version:               info,
		tlsClient:             tlsClient,
		infraAddr:             listener.Addr(),
		infraListener:         listener,
		infraServer:           infraServer,
		pluginDNSToModulePath: pluginDNSToModulePath,
		pluginConnections:     make(map[string]PluginConnection),
	}
	protobuf.RegisterInfraServer(infraServer, &infraServerImpl{
		server:  mux,
		version: info,
	})

	go infraServer.Serve(listener)

	return mux
}

// InfraAddr returns the address of the infra server for plugin-to-server callbacks.
func (s *ServerMux) InfraAddr() net.Addr {
	return s.infraAddr
}

// CACert returns the CA certificate for mutual TLS authentication.
func (s *ServerMux) CACert() *x509.Certificate {
	return s.tlsClient.caCert
}

// SignPluginCSR signs a certificate request for a plugin.
func (s *ServerMux) SignPluginCSR(moduleName string, csr *x509.CertificateRequest) ([]byte, error) {
	return s.tlsClient.SignPluginCSR(moduleName, csr)
}

func (s *ServerMux) RegisterPlugin(target string, moduleName string) (*grpc.ClientConn, error) {
	grpcConn, err := grpc.NewClient(target, grpc.WithTransportCredentials(credentials.NewTLS(s.tlsClient.ClientTLSConfig(moduleName))))
	if err != nil {
		return nil, err
	}
	if _, exists := s.pluginDNSToModulePath[buildPluginTLSName(moduleName)]; exists {
		return nil, fmt.Errorf("plugin %s already registered", moduleName)
	}
	s.pluginDNSToModulePath[buildPluginTLSName(moduleName)] = moduleName
	pluginClient := protobuf.NewPluginClient(grpcConn)
	pluginInfo, err := pluginClient.GetPluginInfo(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	s.pluginConnections[moduleName] = PluginConnection{
		info: pluginInfo,
		conn: grpcConn,
	}
	return grpcConn, nil
}

// GetPluginInfo returns the info of a plugin.
func (s *ServerMux) GetPluginInfo(moduleName string) (*protobuf.Info, error) {
	conn, ok := s.pluginConnections[moduleName]
	if !ok {
		return nil, fmt.Errorf("plugin %s not registered", moduleName)
	}
	return conn.info, nil
}

// GetPluginConnection returns the connection to the plugin for Server-to-Plugin calls.
func (s *ServerMux) GetPluginConnection(moduleName string) (*grpc.ClientConn, error) {
	conn, ok := s.pluginConnections[moduleName]
	if !ok {
		return nil, fmt.Errorf("plugin %s not registered", moduleName)
	}
	return conn.conn, nil
}

func (s *ServerMux) Close() error {
	for _, conn := range s.pluginConnections {
		conn.conn.Close()
	}
	s.infraServer.GracefulStop()
	if s.infraAddr.Network() == "unix" {
		os.Remove(s.infraAddr.String())
	}
	s.infraListener.Close()
	return nil
}
