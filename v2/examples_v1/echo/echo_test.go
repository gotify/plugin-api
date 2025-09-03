package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	papiv1 "github.com/gotify/plugin-api"
	"github.com/gotify/plugin-api/v2"
	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"github.com/gotify/plugin-api/v2/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestEcho(t *testing.T) {
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
		if err := client.Kex(reqFileRx, respFileTx); err != nil {
			panic(err)
		}
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

	listener, addr, err := transport.NewListener()
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

	testUser := &papiv1.UserContext{
		ID:    1,
		Name:  "alice",
		Admin: true,
	}
	webhookBasePath := "/plugin/echo-test/"
	stream, err := pluginClient.RunUserInstance(context.Background(), &protobuf.UserInstanceRequest{
		User: &protobuf.UserContext{
			Id:    uint64(testUser.ID),
			Name:  testUser.Name,
			Admin: testUser.Admin,
		},
		ServerInfo: &protobuf.ServerInfo{
			Version: "1.0.0",
			Capabilities: []protobuf.Capability{
				protobuf.Capability_DISPLAYER,
				protobuf.Capability_CONFIGURER,
				protobuf.Capability_WEBHOOKER,
				protobuf.Capability_MESSENGER,
				protobuf.Capability_STORAGER,
			},
		},
		WebhookBasePath: &webhookBasePath,
		Config:          []byte{},
		Storage:         []byte{},
	})
	if err != nil {
		panic(err)
	}
	defer stream.CloseSend()

	var registeredCapabilities []protobuf.Capability
	var passed bool
	var storage []byte
	expectedMagicString := "hello world"
	ratchet := 1
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}
		switch msg.Update.(type) {
		case *protobuf.InstanceUpdate_Capable:
			registeredCapabilities = append(registeredCapabilities, msg.GetCapable())
		case *protobuf.InstanceUpdate_Message:
			msg := msg.GetMessage()
			extrasNameJson := msg.GetExtras()["plugin::name"].GetJson()
			if extrasNameJson != "\"echo\"" {
				t.Fatal("expected ", "echo", " got ", extrasNameJson)
			}
		case *protobuf.InstanceUpdate_Storage:
			storage = msg.GetStorage()
			var decodedStorage Storage
			json.Unmarshal(storage, &decodedStorage)
			if decodedStorage.CalledTimes < ratchet {
				t.Fatal("expected ", ratchet, " got ", decodedStorage.CalledTimes)
			} else if decodedStorage.CalledTimes == ratchet+1 {
				ratchet++
			} else if decodedStorage.CalledTimes > ratchet+1 {
				t.Fatal("expected ", ratchet+1, " got ", decodedStorage.CalledTimes)
			}
		}

		slices.Sort(registeredCapabilities)
		expectedCapabilities := []protobuf.Capability{
			protobuf.Capability_DISPLAYER,
			protobuf.Capability_CONFIGURER,
			protobuf.Capability_WEBHOOKER,
			protobuf.Capability_MESSENGER,
			protobuf.Capability_STORAGER,
		}
		slices.Sort(expectedCapabilities)
		if reflect.DeepEqual(registeredCapabilities, expectedCapabilities) {

			displayerClient := protobuf.NewDisplayerClient(rpcClient)
			displayResponse, err := displayerClient.Display(context.Background(), &protobuf.DisplayRequest{
				User: &protobuf.UserContext{
					Id:    uint64(testUser.ID),
					Name:  testUser.Name,
					Admin: testUser.Admin,
				},
				Location: "https://gotify.example.com/",
			})
			if err != nil {
				panic(err)
			}
			if displayResponse.GetMarkdown() != "Echo plugin running at: https://gotify.example.com/plugin/echo-test/echo" {
				t.Fatal("expected ", "Echo plugin running at: https://gotify.example.com/plugin/echo-test/echo", " got ", displayResponse.GetMarkdown())
			}

			tlsWebhookName := transport.BuildPluginTLSName(transport.PurposePluginWebhook, pluginInfo.ModulePath)

			for range 3 {
				testreq := httptest.NewRequest("GET", "https://"+tlsWebhookName+"/plugin/echo-test/echo", nil)
				recorder := httptest.NewRecorder()
				compatV1.ServeHTTP(recorder, testreq)
				if recorder.Code != 200 {
					t.Fatal("expected 200 got ", recorder.Code)
				}
				if !strings.Contains(recorder.Body.String(), "Magic string is: "+expectedMagicString) {
					t.Fatal("expected ", "Magic string is: "+expectedMagicString, " got ", recorder.Body.String())
				}
				if !strings.Contains(recorder.Body.String(), "Echo server running at "+webhookBasePath+"echo") {
					t.Fatal("expected ", "Echo server running at "+webhookBasePath+"echo", " got ", recorder.Body.String())
				}
				configurerClient := protobuf.NewConfigurerClient(rpcClient)
				expectedMagicString = fmt.Sprintf("test_%d", ratchet)
				resp, err := configurerClient.ValidateAndSetConfig(context.Background(), &protobuf.ValidateAndSetConfigRequest{
					User: &protobuf.UserContext{
						Id:    uint64(testUser.ID),
						Name:  testUser.Name,
						Admin: testUser.Admin,
					},
					Config: &protobuf.Config{
						Config: "magic_string: " + expectedMagicString,
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if resp.GetResponse().(*protobuf.ValidateAndSetConfigResponse_Success) == nil {
					t.Fatal("expected success")
				}
			}

			passed = true
			_, err = pluginClient.GracefulShutdown(context.Background(), &emptypb.Empty{})
			if err != nil {
				t.Fatal(err)
			}
		}
		if len(registeredCapabilities) > len(expectedCapabilities) {
			t.Fatal("more capabilities than expected")
		}
	}
	if !passed {
		t.Fatal("test failed: connection closed before all capabilities were tested")
	}
	if ratchet != 3 {
		t.Fatal("expected called times to be 3 got ", ratchet)
	}
}
