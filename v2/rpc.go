package plugin

import (
	"context"
	"net"

	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

// PluginRpcHandler handles and coordinates RPC between the plugin and the server.
type PluginRpcHandler struct {
	infraTarget string
	server      *grpc.Server
}

func NewPluginRpcHandler(infraTarget string) *PluginRpcHandler {
	infraClient, err := grpc.NewClient(infraTarget)
	if err != nil {
		panic(err)
	}
	infraRpcClient := protobuf.NewInfraClient(infraClient)
	version, err := infraRpcClient.GetServerVersion(context.Background(), &emptypb.Empty{})
	if err != nil {
		panic(err)
	}
	_ = version

	return &PluginRpcHandler{
		infraTarget: infraTarget,
		server:      grpc.NewServer(),
	}
}

func (h *PluginRpcHandler) Serve(listener net.Listener) error {
	return h.server.Serve(listener)
}
