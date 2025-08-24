package plugin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log"
	"math"
	"net/http"
	"net/url"
	"plugin"
	"slices"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"gopkg.in/yaml.v2"

	"github.com/gin-gonic/gin"
	papiv1 "github.com/gotify/plugin-api"
	"github.com/gotify/plugin-api/v2/generated/protobuf"
)

type GrpcDialer interface {
	Dial(ctx context.Context) (*grpc.ClientConn, error)
}

// CompatV1 is a shim that acts like a plugin server and delegates request to
// something that implements a V1-style API interface.
type CompatV1 struct {
	GetPluginInfo func() *papiv1.Info
	GetInstance   func(user *papiv1.UserContext) (papiv1.Plugin, error)
}

// NewCompatV1FromPlugin creates a new CompatV1 from a native Go plugin.
func NewCompatV1FromPlugin(plugin *plugin.Plugin) (*CompatV1, error) {
	getPluginInfo, err := plugin.Lookup("GetGotifyPluginInfo")
	if err != nil {
		return nil, err
	}
	getInstance, err := plugin.Lookup("NewGotifyPlugin")
	if err != nil {
		return nil, err
	}

	getPluginInfoChecked, ok := getPluginInfo.(func() *papiv1.Info)
	if !ok {
		return nil, errors.New("GetGotifyPluginInfo is not a function")
	}
	getInstanceCheckedWithErr, ok := getInstance.(func(user *papiv1.UserContext) (papiv1.Plugin, error))
	if !ok {
		if getInstanceCheckedWithoutErr, ok := getInstance.(func(user *papiv1.UserContext) papiv1.Plugin); ok {
			getInstanceCheckedWithErr = func(user *papiv1.UserContext) (papiv1.Plugin, error) {
				return getInstanceCheckedWithoutErr(user), nil
			}
		} else {
			return nil, errors.New("NewGotifyPlugin is not a function")
		}
	}

	return &CompatV1{
		GetPluginInfo: getPluginInfoChecked,
		GetInstance:   getInstanceCheckedWithErr,
	}, nil
}

// CompatV1Shim is a shim that acts like a plugin server and delegates request to
// something that implements a V1-style API interface.
type CompatV1Shim struct {
	mu           *sync.RWMutex
	compatV1     *CompatV1
	gin          *gin.Engine
	instances    map[uint64]papiv1.Plugin
	pluginServer *grpc.Server
	pluginInfo   *papiv1.Info
	http.Server
}

// NewCompatV1Rpc creates a new CompatV1Shim server.
func NewCompatV1Rpc(compatV1 *CompatV1, cliArgs []string) (*CompatV1Shim, error) {
	pluginInfo := compatV1.GetPluginInfo()
	tlsName := BuildPluginTLSName(purposePluginRPC, pluginInfo.Name)

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
		ClientCAs:  rootCAs,
	}

	rpcServer := grpc.NewServer()

	gin := gin.Default()

	self := &CompatV1Shim{
		mu:           &sync.RWMutex{},
		instances:    make(map[uint64]papiv1.Plugin),
		compatV1:     compatV1,
		gin:          gin,
		pluginServer: rpcServer,
		pluginInfo:   pluginInfo,
	}

	selfServer := &compatV1ShimServer{
		shim: self,
	}

	protobuf.RegisterPluginServer(rpcServer, selfServer)
	protobuf.RegisterDisplayerServer(rpcServer, selfServer)
	protobuf.RegisterConfigurerServer(rpcServer, selfServer)

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	self.Server = http.Server{
		Handler:   self,
		TLSConfig: tlsConfig,
		Protocols: protocols,
	}

	return self, nil
}

func (h *CompatV1Shim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "Must use TLS", http.StatusUpgradeRequired)
		return
	}

	pluginRpcHostName := BuildPluginTLSName(purposePluginRPC, h.pluginInfo.ModulePath)

	if r.TLS.ServerName == pluginRpcHostName {
		if r.ProtoMajor != 2 {
			http.Error(w, "Must use HTTP/2", http.StatusHTTPVersionNotSupported)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			http.Error(w, "Must use application/grpc content type", http.StatusUnsupportedMediaType)
			return
		}
		h.pluginServer.ServeHTTP(w, r)

		return
	}

	pluginWebhookHostName := BuildPluginTLSName(purposePluginWebhook, h.pluginInfo.ModulePath)
	if r.TLS.ServerName == pluginWebhookHostName {
		h.gin.ServeHTTP(w, r)
		return
	}

	http.Error(w, "Virtual host not found", http.StatusNotFound)
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

type compatV1ShimServer struct {
	shim *CompatV1Shim
	protobuf.UnimplementedPluginServer
	protobuf.UnimplementedDisplayerServer
	protobuf.UnimplementedConfigurerServer
}

func (s *compatV1ShimServer) GetPluginInfo(ctx context.Context, req *emptypb.Empty) (*protobuf.Info, error) {
	return &protobuf.Info{
		Version:     s.shim.pluginInfo.Version,
		Author:      s.shim.pluginInfo.Author,
		Name:        s.shim.pluginInfo.Name,
		Website:     s.shim.pluginInfo.Website,
		Description: s.shim.pluginInfo.Description,
		License:     s.shim.pluginInfo.License,
		ModulePath:  s.shim.pluginInfo.ModulePath,
	}, nil
}

func (s *compatV1ShimServer) SetEnable(ctx context.Context, req *protobuf.SetEnableRequest) (*emptypb.Empty, error) {
	s.shim.mu.RLock()
	instance, ok := s.shim.instances[req.User.Id]
	s.shim.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	if req.Enable {
		return new(emptypb.Empty), instance.Enable()
	} else {
		return new(emptypb.Empty), instance.Disable()
	}
}

func (s *compatV1ShimServer) Display(ctx context.Context, req *protobuf.DisplayRequest) (*protobuf.DisplayResponse, error) {
	s.shim.mu.RLock()
	instance, ok := s.shim.instances[req.User.Id]
	s.shim.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	if displayer, ok := instance.(papiv1.Displayer); ok {
		location, err := url.Parse(req.Location)
		if err != nil {
			return nil, err
		}
		return &protobuf.DisplayResponse{
			Response: &protobuf.DisplayResponse_Markdown{
				Markdown: displayer.GetDisplay(location),
			},
		}, nil
	}
	return nil, errors.New("instance does not implement displayer")
}

func (s *compatV1ShimServer) DefaultConfig(ctx context.Context, req *protobuf.DefaultConfigRequest) (*protobuf.Config, error) {
	s.shim.mu.RLock()
	instance, ok := s.shim.instances[req.User.Id]
	s.shim.mu.RUnlock()
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

func (s *compatV1ShimServer) ValidateAndSetConfig(ctx context.Context, req *protobuf.ValidateAndSetConfigRequest) (*protobuf.ValidateAndSetConfigResponse, error) {
	s.shim.mu.RLock()
	instance, ok := s.shim.instances[req.User.Id]
	s.shim.mu.RUnlock()
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
				Response: &protobuf.ValidateAndSetConfigResponse_ValidationError{
					ValidationError: &protobuf.Error{
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

func (s *compatV1ShimServer) RunUserInstance(req *protobuf.UserInstanceRequest, stream protobuf.Plugin_RunUserInstanceServer) error {
	if req.User.Id > math.MaxUint {
		return errors.New("user id is too large")
	}
	instance, err := s.shim.compatV1.GetInstance(&papiv1.UserContext{
		ID:    uint(req.User.Id),
		Name:  req.User.Name,
		Admin: req.User.Admin,
	})
	if err != nil {
		return err
	}

	// enable supported capabilities
	if _, ok := instance.(papiv1.Displayer); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_DISPLAYER) {
			stream.Send(&protobuf.InstanceUpdate{
				Update: &protobuf.InstanceUpdate_Capable{
					Capable: protobuf.Capability_DISPLAYER,
				},
			})
		} else {
			return errors.New("displayer not supported by server but V1 API does not support backwards compatibility")
		}
	}
	if _, ok := instance.(papiv1.Messenger); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_MESSENGER) {
			stream.Send(&protobuf.InstanceUpdate{
				Update: &protobuf.InstanceUpdate_Capable{
					Capable: protobuf.Capability_MESSENGER,
				},
			})
		} else {
			return errors.New("messenger not supported by server but V1 API does not support backwards compatibility")
		}
	}
	if _, ok := instance.(papiv1.Configurer); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_CONFIGURER) {
			stream.Send(&protobuf.InstanceUpdate{
				Update: &protobuf.InstanceUpdate_Capable{
					Capable: protobuf.Capability_CONFIGURER,
				},
			})
		} else {
			return errors.New("configurer not supported by server but V1 API does not support backwards compatibility")
		}
	}
	if _, ok := instance.(papiv1.Storager); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_STORAGER) {
			stream.Send(&protobuf.InstanceUpdate{
				Update: &protobuf.InstanceUpdate_Capable{
					Capable: protobuf.Capability_STORAGER,
				},
			})
		} else {
			return errors.New("storager not supported by server but V1 API does not support backwards compatibility")
		}
	}
	if _, ok := instance.(papiv1.Webhooker); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_WEBHOOKER) {
			stream.Send(&protobuf.InstanceUpdate{
				Update: &protobuf.InstanceUpdate_Capable{
					Capable: protobuf.Capability_WEBHOOKER,
				},
			})
		} else {
			return errors.New("webhooker not supported by server but V1 API does not support backwards compatibility")
		}
	}

	if messenger, ok := instance.(papiv1.Messenger); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_MESSENGER) {
			messenger.SetMessageHandler(&shimV1MessageHandler{
				stream: &stream,
			})
		} else {
			return errors.New("messenger not supported by server but V1 API does not support backwards compatibility")
		}
	}

	if configurer, ok := instance.(papiv1.Configurer); ok {
		if slices.Contains(req.ServerVersion.Capabilities, protobuf.Capability_CONFIGURER) {
			currentConfig := configurer.DefaultConfig()
			if req.Config != nil {
				yaml.Unmarshal(req.Config, &currentConfig)
				if err := configurer.ValidateAndSetConfig(currentConfig); err != nil {
					return err
				}
			}
		} else {
			return errors.New("configurer not supported by server but V1 API does not support backwards compatibility")
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
			group := s.shim.gin.Group(*req.WebhookBasePath)
			webhooker.RegisterWebhook(*req.WebhookBasePath, group)
		}
	}

	s.shim.mu.Lock()
	s.shim.instances[req.User.Id] = instance
	s.shim.mu.Unlock()

	return nil
}
