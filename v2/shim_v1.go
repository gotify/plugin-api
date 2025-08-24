package plugin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/emptypb"
	"gopkg.in/yaml.v2"

	"github.com/gin-gonic/gin"
	papiv1 "github.com/gotify/plugin-api"
	"github.com/gotify/plugin-api/v2/generated/protobuf"
)

type GrpcDialer interface {
	Dial(ctx context.Context) (*grpc.ClientConn, error)
}

type CompatV1 struct {
	GetPluginInfo func() *papiv1.Info
	GetInstance   func(user *papiv1.UserContext) (papiv1.Plugin, error)
}

type PluginShim struct {
	mu           *sync.RWMutex
	compatV1     *CompatV1
	gin          *gin.Engine
	instances    map[uint64]papiv1.Plugin
	pluginServer *grpc.Server
	pluginInfo   *papiv1.Info
	protobuf.UnimplementedPluginServer
	protobuf.UnimplementedDisplayerServer
	protobuf.UnimplementedConfigurerServer
}

func (s *PluginShim) GetPluginInfo(ctx context.Context, req *emptypb.Empty) (*protobuf.Info, error) {
	return &protobuf.Info{
		Version:     s.pluginInfo.Version,
		Author:      s.pluginInfo.Author,
		Name:        s.pluginInfo.Name,
		Website:     s.pluginInfo.Website,
		Description: s.pluginInfo.Description,
		License:     s.pluginInfo.License,
		ModulePath:  s.pluginInfo.ModulePath,
	}, nil
}

type shimV1MessageHandler struct {
	stream *protobuf.Plugin_RunUserInstanceServer
}

func (h *shimV1MessageHandler) SendMessage(msg papiv1.Message) error {
	return (*h.stream).Send(&protobuf.InstanceUpdate{
		Update: &protobuf.InstanceUpdate_Message{
			Message: &protobuf.Message{
				Message: msg.Message,
			},
		},
	})
}

type shimV1StorageHandler struct {
	currentStorage []byte
	stream         *protobuf.Plugin_RunUserInstanceServer
}

func (h *shimV1StorageHandler) Save(b []byte) error {
	return (*h.stream).Send(&protobuf.InstanceUpdate{
		Update: &protobuf.InstanceUpdate_Storage{
			Storage: b,
		},
	})
}

func (h *shimV1StorageHandler) Load() (b []byte, err error) {
	copy(h.currentStorage, b)
	return
}

func (s *PluginShim) SetEnable(ctx context.Context, req *protobuf.SetEnableRequest) (*emptypb.Empty, error) {
	if req.User.Id > math.MaxUint {
		return nil, errors.New("user id is too large")
	}
	s.mu.RLock()
	instance, ok := s.instances[uint64(req.User.Id)]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	if req.Enable {
		return new(emptypb.Empty), instance.Enable()
	} else {
		return new(emptypb.Empty), instance.Disable()
	}
}

func (s *PluginShim) Display(ctx context.Context, req *protobuf.DisplayRequest) (*protobuf.DisplayResponse, error) {
	if req.User.Id > math.MaxUint {
		return nil, errors.New("user id is too large")
	}
	s.mu.RLock()
	instance, ok := s.instances[uint64(req.User.Id)]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	if displayer, ok := instance.(papiv1.Displayer); ok {
		location, err := url.Parse(req.Location)
		if err != nil {
			return nil, err
		}
		return &protobuf.DisplayResponse{
			Display: displayer.GetDisplay(location),
		}, nil
	}
	return nil, errors.New("instance does not implement displayer")
}

func (s *PluginShim) DefaultConfig(ctx context.Context, req *protobuf.DefaultConfigRequest) (*protobuf.Config, error) {
	if req.User.Id > math.MaxUint {
		return nil, errors.New("user id is too large")
	}
	s.mu.RLock()
	instance, ok := s.instances[uint64(req.User.Id)]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	if configurer, ok := instance.(papiv1.Configurer); ok {
		defaultConfig := configurer.DefaultConfig()
		bytes, err := yaml.Marshal(defaultConfig)
		if err != nil {
			return nil, err
		}
		return &protobuf.Config{
			Config: string(bytes),
		}, nil
	}
	return nil, errors.New("instance does not implement configurer")
}

func (s *PluginShim) ValidateAndSetConfig(ctx context.Context, req *protobuf.ValidateAndSetConfigRequest) (*protobuf.ValidateAndSetConfigResponse, error) {
	if req.User.Id > math.MaxUint {
		return nil, errors.New("user id is too large")
	}
	s.mu.RLock()
	instance, ok := s.instances[uint64(req.User.Id)]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	if configurer, ok := instance.(papiv1.Configurer); ok {
		var currentConfig interface{}
		if req.Config != nil {
			yaml.Unmarshal([]byte(req.Config.Config), &currentConfig)
		}
		if err := configurer.ValidateAndSetConfig(currentConfig); err != nil {
			return &protobuf.ValidateAndSetConfigResponse{
				Response: &protobuf.ValidateAndSetConfigResponse_Error{
					Error: &protobuf.Error{
						Message: err.Error(),
					},
				},
			}, nil
		}
		return &protobuf.ValidateAndSetConfigResponse{
			Response: &protobuf.ValidateAndSetConfigResponse_Success{
				Success: new(emptypb.Empty),
			},
		}, nil
	}
	return nil, errors.New("instance does not implement configurer")
}

func (s *PluginShim) RunUserInstance(req *protobuf.UserInstanceRequest, stream protobuf.Plugin_RunUserInstanceServer) error {
	if req.User.Id > math.MaxUint {
		return errors.New("user id is too large")
	}
	instance, err := s.compatV1.GetInstance(&papiv1.UserContext{
		ID:    uint(req.User.Id),
		Name:  req.User.Name,
		Admin: req.User.Admin,
	})
	if err != nil {
		return err
	}

	// enable supported capabilities
	var capabilities []protobuf.Capability
	if _, ok := instance.(papiv1.Displayer); ok {
		capabilities = append(capabilities, protobuf.Capability_DISPLAYER)
	}
	if _, ok := instance.(papiv1.Messenger); ok {
		capabilities = append(capabilities, protobuf.Capability_MESSENGER)
	}
	if _, ok := instance.(papiv1.Configurer); ok {
		capabilities = append(capabilities, protobuf.Capability_CONFIGURER)
	}
	if _, ok := instance.(papiv1.Storager); ok {
		capabilities = append(capabilities, protobuf.Capability_STORAGER)
	}
	if _, ok := instance.(papiv1.Webhooker); ok {
		capabilities = append(capabilities, protobuf.Capability_WEBHOOKER)
	}

	if err := stream.Send(&protobuf.InstanceUpdate{
		Update: &protobuf.InstanceUpdate_Capabilities{
			Capabilities: &protobuf.PluginCapabilities{
				Capabilities: capabilities,
			},
		},
	}); err != nil {
		return err
	}

	if messenger, ok := instance.(papiv1.Messenger); ok {
		messenger.SetMessageHandler(&shimV1MessageHandler{
			stream: &stream,
		})
	}

	if configurer, ok := instance.(papiv1.Configurer); ok {
		currentConfig := configurer.DefaultConfig()
		if req.Config != nil {
			yaml.Unmarshal(req.Config, &currentConfig)
			if err := configurer.ValidateAndSetConfig(currentConfig); err != nil {
				return err
			}
		}
	}

	if storager, ok := instance.(papiv1.Storager); ok {
		storageHandler := &shimV1StorageHandler{
			currentStorage: req.Storage,
			stream:         &stream,
		}
		storager.SetStorageHandler(storageHandler)
	}

	if webhooker, ok := instance.(papiv1.Webhooker); ok {
		if req.WebhookBasePath != nil {
			group := s.gin.Group(*req.WebhookBasePath)
			webhooker.RegisterWebhook(*req.WebhookBasePath, group)
		}
	}

	s.mu.Lock()
	s.instances[uint64(req.User.Id)] = instance
	s.mu.Unlock()

	return nil
}

func NewPluginRpc(compatV1 *CompatV1, cliArgs []string) (*PluginShim, error) {
	pluginInfo := compatV1.GetPluginInfo()
	tlsName := BuildPluginTLSName(pluginInfo.Name)

	cliFlags, err := ParsePluginCLIFlags(cliArgs)
	if err != nil {
		log.Fatalf("Failed to parse CLI flags: %v", err)
	}
	rootCAs := x509.NewCertPool()
	caCert, err := x509.ParseCertificate(cliFlags.CAData)
	if err != nil {
		return nil, err
	}
	rootCAs.AddCert(caCert)

	leafCert, err := x509.ParseCertificate(cliFlags.CertData)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{cliFlags.CertData},
				PrivateKey:  cliFlags.KeyData,
				Leaf:        leafCert,
			},
			{
				Certificate: [][]byte{caCert.Raw},
			},
		},
		RootCAs:    rootCAs,
		ServerName: tlsName,
		ClientAuth: tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if state.ServerName != ServerTLSName {
				return errors.New("not implemented: client must be the gotify server itself for now")
			}
			return nil
		},
	}

	rpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(tlsConfig)))

	gin := gin.Default()

	self := &PluginShim{
		mu:           &sync.RWMutex{},
		instances:    make(map[uint64]papiv1.Plugin),
		compatV1:     compatV1,
		gin:          gin,
		pluginServer: rpcServer,
		pluginInfo:   pluginInfo,
	}

	protobuf.RegisterPluginServer(rpcServer, self)
	protobuf.RegisterDisplayerServer(rpcServer, self)
	protobuf.RegisterConfigurerServer(rpcServer, self)

	return self, nil
}

func (h *PluginShim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pluginHostName := BuildPluginTLSName(h.pluginInfo.ModulePath)
	if r.Host == pluginHostName {
		if r.ProtoMajor != 2 {
			http.Error(w, "Must use HTTP/2", http.StatusHTTPVersionNotSupported)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.Error(w, "Must use application/grpc content type", http.StatusUnsupportedMediaType)
		}
		h.pluginServer.ServeHTTP(w, r)
		return
	}

	if h.gin != nil {
		h.gin.ServeHTTP(w, r)
		return
	}
}

func (h *PluginShim) Serve(listener net.Listener) error {
	return h.pluginServer.Serve(listener)
}
