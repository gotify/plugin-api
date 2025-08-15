package plugin

import (
	"context"
	"net"

	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"google.golang.org/grpc"
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
