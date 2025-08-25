package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"testing"

	papiv1 "github.com/gotify/plugin-api"
	"github.com/gotify/plugin-api/v2"
	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"github.com/gotify/plugin-api/v2/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"
)

func testEchoImpl(t *testing.T, listener net.Listener, addr string) {
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

	compatV1, err := plugin.NewCompatV1Rpc(&plugin.CompatV1{
		GetPluginInfo: GetGotifyPluginInfo,
		GetInstance: func(user papiv1.UserContext) (papiv1.Plugin, error) {
			return NewGotifyPluginInstance(user), nil
		},
	}, []string{
		"-kex-req-file", fmt.Sprintf("/proc/self/fd/%d", reqTx),
		"-kex-resp-file", fmt.Sprintf("/proc/self/fd/%d", respRx),
	})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		compatV1.ServeTLS(listener, "", "")
	}()

	rpcClient, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(client.ClientTLSConfig(pluginInfo.ModulePath))))
	if err != nil {
		t.Fatal(err)
	}

	pluginClient := protobuf.NewPluginClient(rpcClient)
	version, err := pluginClient.GetPluginInfo(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if version.Name != pluginInfo.Name {
		t.Fatal("expected ", pluginInfo.Name, " got ", version.Name)
	}
	if version.Version != pluginInfo.Version {
		t.Fatal("expected ", pluginInfo.Version, " got ", version.Version)
	}

	pluginClient.SetEnable(context.Background(), &protobuf.SetEnableRequest{
		User: &protobuf.UserContext{
			Id:    uint64(1),
			Name:  "test",
			Admin: false,
		},
		Enable: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	stream, err := pluginClient.RunUserInstance(context.Background(), &protobuf.UserInstanceRequest{
		User: &protobuf.UserContext{
			Id:    uint64(1),
			Name:  "test",
			Admin: false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	pluginClient.GracefulShutdown(context.Background(), &emptypb.Empty{})
	stream.CloseSend()
	for {
		_, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
	}
}

func TestEcho(t *testing.T) {
	listener, addr, err := transport.NewListener()
	if err != nil {
		t.Fatal(err)
	}
	testEchoImpl(t, listener, addr)
}

func TestEchoTCP(t *testing.T) {
	listener, addr, err := transport.NewTCPListener()
	if err != nil {
		t.Fatal(err)
	}
	testEchoImpl(t, listener, addr)
}
