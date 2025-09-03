package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	papiv1 "github.com/gotify/plugin-api"
	"github.com/gotify/plugin-api/v2"
	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"github.com/gotify/plugin-api/v2/transport"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/emptypb"
)

func testMinimalImpl(t *testing.T, listener net.Listener, addr string) {
	assert := assert.New(t)

	pluginInfo := GetGotifyPluginInfo()

	client, err := transport.NewEphemeralTLSClient()
	if err != nil {
		t.Fatal(err)
	}

	var reqRx, reqTx uintptr
	var respRx, respTx uintptr

	transport.NewAnonPipe(&reqRx, &reqTx, true)
	transport.NewAnonPipe(&respRx, &respTx, true)

	go func() {
		reqFileRx := os.NewFile(reqRx, fmt.Sprintf("/proc/self/fd/%d", reqRx))
		defer reqFileRx.Close()
		respFileTx := os.NewFile(respTx, fmt.Sprintf("/proc/self/fd/%d", respTx))
		defer respFileTx.Close()
		client.Kex(reqFileRx, respFileTx)
	}()

	var thisInstance *Plugin
	instanceInitialized := make(chan struct{})

	compatV1, err := plugin.NewCompatV1Rpc(&plugin.CompatV1{
		GetPluginInfo: GetGotifyPluginInfo,
		GetInstance: func(user papiv1.UserContext) (papiv1.Plugin, error) {
			thisInstance = NewGotifyPluginInstance(user).(*Plugin)
			defer close(instanceInitialized)
			return thisInstance, nil
		},
	}, []string{
		"-kex-req-file", fmt.Sprintf("/proc/self/fd/%d", reqTx),
		"-kex-resp-file", fmt.Sprintf("/proc/self/fd/%d", respRx),
	})
	assert.NoError(err)

	go func() {
		compatV1.ServeTLS(listener, "", "")
	}()

	rpcClient, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(client.ClientTLSConfig(pluginInfo.ModulePath))), grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                10 * time.Millisecond,
		PermitWithoutStream: true,
	}))
	assert.NoError(err)

	pluginClient := protobuf.NewPluginClient(rpcClient)
	version, err := pluginClient.GetPluginInfo(context.Background(), &emptypb.Empty{})
	assert.NoError(err)
	assert.Equal(pluginInfo.Name, version.Name)
	assert.Equal(pluginInfo.Version, version.Version)

	stream, err := pluginClient.RunUserInstance(context.Background(), &protobuf.UserInstanceRequest{
		User: &protobuf.UserContext{
			Id:    uint64(1),
			Name:  "test",
			Admin: false,
		},
	})
	assert.NoError(err)

	assert.NoError(stream.CloseSend())
	<-instanceInitialized
	assert.True(thisInstance.enabled, "plugin should be enabled after connect")

	pluginClient.GracefulShutdown(context.Background(), &emptypb.Empty{})
	for {
		_, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
	}

	streamHang, err := pluginClient.RunUserInstance(context.Background(), &protobuf.UserInstanceRequest{
		User: &protobuf.UserContext{
			Id:    uint64(1),
			Name:  "test",
			Admin: false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := streamHang.CloseSend(); err != nil {
		t.Fatal(err)
	}
	_, err = streamHang.Recv()
	assert.Error(err, "expected error when not sending keepalive")
	assert.False(thisInstance.enabled, "plugin should be disabled after hang")

	streamReentrant, err := pluginClient.RunUserInstance(context.Background(), &protobuf.UserInstanceRequest{
		User: &protobuf.UserContext{
			Id:    uint64(1),
			Name:  "test",
			Admin: false,
		},
	})
	assert.NoError(err)
	pluginClient.GracefulShutdown(context.Background(), &emptypb.Empty{})
	if err := streamReentrant.CloseSend(); err != nil {
		assert.NoError(err)
	}
	for {
		_, err := streamReentrant.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			assert.NoError(err)
		}
	}
}

func TestMinimal(t *testing.T) {
	listener, addr, err := transport.NewListener()
	if err != nil {
		t.Fatal(err)
	}
	testMinimalImpl(t, listener, addr)
}

func TestMinimalTCP(t *testing.T) {
	listener, addr, err := transport.NewTCPListener()
	if err != nil {
		t.Fatal(err)
	}
	testMinimalImpl(t, listener, addr)
}
