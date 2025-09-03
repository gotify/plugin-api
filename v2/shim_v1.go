package plugin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"net/url"
	"plugin"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/emptypb"
	"gopkg.in/yaml.v3"

	"github.com/gin-gonic/gin"
	papiv1 "github.com/gotify/plugin-api"
	"github.com/gotify/plugin-api/v2/generated/protobuf"
	"github.com/gotify/plugin-api/v2/transport"
)

type GrpcDialer interface {
	Dial(ctx context.Context) (*grpc.ClientConn, error)
}

var (
	httpTimeout = 10 * time.Second
	pingRate    = 4 * time.Second
)

func init() {
	if testing.Testing() {
		httpTimeout = 100 * time.Millisecond
		pingRate = 10 * time.Millisecond
	}
}

// CompatV1 is an API interface that is compatible with the V1 API.
type CompatV1 struct {
	GetPluginInfo func() papiv1.Info
	GetInstance   func(user papiv1.UserContext) (papiv1.Plugin, error)
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

	getPluginInfoChecked, ok := getPluginInfo.(func() papiv1.Info)
	if !ok {
		return nil, errors.New("GetGotifyPluginInfo is not a function")
	}
	getInstanceCheckedWithErr, ok := getInstance.(func(user papiv1.UserContext) (papiv1.Plugin, error))
	if !ok {
		if getInstanceCheckedWithoutErr, ok := getInstance.(func(user papiv1.UserContext) papiv1.Plugin); ok {
			getInstanceCheckedWithErr = func(user papiv1.UserContext) (papiv1.Plugin, error) {
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
	shutdown     chan struct{}
	shutdownOnce *sync.Once
	mu           *sync.RWMutex
	compatV1     *CompatV1
	gin          *gin.Engine
	instances    map[uint64]papiv1.Plugin
	pluginServer *grpc.Server
	pluginInfo   papiv1.Info
	http.Server
}

// NewCompatV1Rpc creates a new CompatV1Shim server.
func NewCompatV1Rpc(compatV1 *CompatV1, cliArgs []string) (*CompatV1Shim, error) {
	pluginInfo := compatV1.GetPluginInfo()

	cli, err := ParsePluginCli(cliArgs)
	if err != nil {
		log.Fatalf("Failed to parse CLI flags: %v", err)
	}
	defer cli.Close()

	rootCAs := x509.NewCertPool()
	certificateChain, err := cli.Kex(pluginInfo.ModulePath, rootCAs)

	tlsConfig := &tls.Config{
		Certificates: certificateChain,
		RootCAs:      rootCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    rootCAs,
	}

	rpcServer := grpc.NewServer(grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             httpTimeout,
		PermitWithoutStream: true,
	}), grpc.ConnectionTimeout(httpTimeout))
	if !cli.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	ginEngine := gin.Default()

	self := &CompatV1Shim{
		shutdown:     make(chan struct{}),
		shutdownOnce: &sync.Once{},
		mu:           &sync.RWMutex{},
		instances:    make(map[uint64]papiv1.Plugin),
		compatV1:     compatV1,
		gin:          ginEngine,
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
		Handler:           self,
		TLSConfig:         tlsConfig,
		Protocols:         protocols,
		ReadTimeout:       httpTimeout,
		ReadHeaderTimeout: httpTimeout,
		WriteTimeout:      httpTimeout,
	}

	return self, nil
}

func (h *CompatV1Shim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "Must use TLS", http.StatusUpgradeRequired)
		return
	}

	pluginRpcHostName := transport.BuildPluginTLSName(transport.PurposePluginRPC, h.pluginInfo.ModulePath)

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

	pluginWebhookHostName := transport.BuildPluginTLSName(transport.PurposePluginWebhook, h.pluginInfo.ModulePath)
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
	extras := make(map[string]*protobuf.ExtrasValue)
	for k, v := range msg.Extras {
		jsonValue, err := json.Marshal(v)
		if err != nil {
			return err
		}
		extras[k] = &protobuf.ExtrasValue{
			Value: &protobuf.ExtrasValue_Json{
				Json: string(jsonValue),
			},
		}
	}
	return (*h.stream).Send(&protobuf.InstanceUpdate{
		Update: &protobuf.InstanceUpdate_Message{
			Message: &protobuf.Message{
				Message:  msg.Message,
				Title:    msg.Title,
				Priority: int32(msg.Priority),
				Extras:   extras,
			},
		},
	})
}

type shimV1StorageHandler struct {
	mutex          *sync.RWMutex
	currentStorage []byte
	stream         *protobuf.Plugin_RunUserInstanceServer
}

func (h *shimV1StorageHandler) Save(b []byte) error {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.currentStorage = slices.Clone(b)
	return (*h.stream).Send(&protobuf.InstanceUpdate{
		Update: &protobuf.InstanceUpdate_Storage{
			Storage: b,
		},
	})
}

func (h *shimV1StorageHandler) Load() (b []byte, err error) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	b = slices.Clone(h.currentStorage)
	return
}

type compatV1ShimServer struct {
	shim *CompatV1Shim
	protobuf.UnimplementedPluginServer
	protobuf.UnimplementedDisplayerServer
	protobuf.UnimplementedConfigurerServer
}

func (s *CompatV1Shim) getInstanceByUserId(userId uint64) (papiv1.Plugin, error) {
	s.mu.RLock()
	instance, ok := s.instances[userId]
	s.mu.RUnlock()
	if !ok {
		return nil, errors.New("instance not found")
	}
	return instance, nil
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

func (s *compatV1ShimServer) Display(ctx context.Context, req *protobuf.DisplayRequest) (*protobuf.DisplayResponse, error) {
	instance, err := s.shim.getInstanceByUserId(req.User.Id)
	if err != nil {
		return nil, err
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
	instance, err := s.shim.getInstanceByUserId(req.User.Id)
	if err != nil {
		return nil, err
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
	instance, err := s.shim.getInstanceByUserId(req.User.Id)
	if err != nil {
		return nil, err
	}
	if configurer, ok := instance.(papiv1.Configurer); ok {
		currentConfig := configurer.DefaultConfig()
		if req.Config != nil {
			if reflect.TypeOf(currentConfig).Kind() == reflect.Pointer {
				yaml.Unmarshal([]byte(req.Config.Config), currentConfig)
			} else {
				yaml.Unmarshal([]byte(req.Config.Config), &currentConfig)
			}
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

func (s *compatV1ShimServer) GracefulShutdown(ctx context.Context, req *emptypb.Empty) (*emptypb.Empty, error) {
	s.shim.shutdownOnce.Do(func() {
		close(s.shim.shutdown)
	})
	return &emptypb.Empty{}, nil
}

func (s *compatV1ShimServer) RunUserInstance(req *protobuf.UserInstanceRequest, stream protobuf.Plugin_RunUserInstanceServer) error {
	if req.User.Id > math.MaxUint {
		return errors.New("user id is too large")
	}

	unlockOnce := new(sync.Once)

	s.shim.mu.Lock()

	defer unlockOnce.Do(func() {
		s.shim.mu.Unlock()
	})

	instance, alreadyRunning := s.shim.instances[req.User.Id]

	if !alreadyRunning {
		var err error
		instance, err = s.shim.compatV1.GetInstance(papiv1.UserContext{
			ID:    uint(req.User.Id),
			Name:  req.User.Name,
			Admin: req.User.Admin,
		})
		if err != nil {
			return err
		}

		// enable supported capabilities
		if _, ok := instance.(papiv1.Displayer); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_DISPLAYER) {
				if err := stream.Send(&protobuf.InstanceUpdate{
					Update: &protobuf.InstanceUpdate_Capable{
						Capable: protobuf.Capability_DISPLAYER,
					},
				}); err != nil {
					return err
				}
			} else {
				return errors.New("displayer not supported by server but V1 API does not support backwards compatibility")
			}
		}
		if _, ok := instance.(papiv1.Messenger); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_MESSENGER) {
				if err := stream.Send(&protobuf.InstanceUpdate{
					Update: &protobuf.InstanceUpdate_Capable{
						Capable: protobuf.Capability_MESSENGER,
					},
				}); err != nil {
					return err
				}
			} else {
				return errors.New("messenger not supported by server but V1 API does not support backwards compatibility")
			}
		}
		if _, ok := instance.(papiv1.Configurer); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_CONFIGURER) {
				if err := stream.Send(&protobuf.InstanceUpdate{
					Update: &protobuf.InstanceUpdate_Capable{
						Capable: protobuf.Capability_CONFIGURER,
					},
				}); err != nil {
					return err
				}
			} else {
				return errors.New("configurer not supported by server but V1 API does not support backwards compatibility")
			}
		}
		if _, ok := instance.(papiv1.Storager); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_STORAGER) {
				if err := stream.Send(&protobuf.InstanceUpdate{
					Update: &protobuf.InstanceUpdate_Capable{
						Capable: protobuf.Capability_STORAGER,
					},
				}); err != nil {
					return err
				}
			} else {
				return errors.New("storager not supported by server but V1 API does not support backwards compatibility")
			}
		}
		if _, ok := instance.(papiv1.Webhooker); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_WEBHOOKER) {
				if err := stream.Send(&protobuf.InstanceUpdate{
					Update: &protobuf.InstanceUpdate_Capable{
						Capable: protobuf.Capability_WEBHOOKER,
					},
				}); err != nil {
					return err
				}
			} else {
				return errors.New("webhooker not supported by server but V1 API does not support backwards compatibility")
			}
		}

		if messenger, ok := instance.(papiv1.Messenger); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_MESSENGER) {
				messenger.SetMessageHandler(&shimV1MessageHandler{
					stream: &stream,
				})
			} else {
				return errors.New("messenger not supported by server but V1 API does not support backwards compatibility")
			}
		}

		if configurer, ok := instance.(papiv1.Configurer); ok {
			if slices.Contains(req.ServerInfo.Capabilities, protobuf.Capability_CONFIGURER) {
				currentConfig := configurer.DefaultConfig()
				if req.Config != nil {
					if err := yaml.Unmarshal(req.Config, &currentConfig); err != nil {
						return err
					}
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
				mutex:          &sync.RWMutex{},
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
	}

	if err := instance.Enable(); err != nil {
		return err
	}

	defer instance.Disable()

	s.shim.instances[req.User.Id] = instance
	unlockOnce.Do(func() {
		s.shim.mu.Unlock()
	})

	ticker := time.NewTicker(pingRate)

	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := stream.Send(&protobuf.InstanceUpdate{
				Update: &protobuf.InstanceUpdate_Ping{
					Ping: new(emptypb.Empty),
				},
			}); err != nil {
				return err
			}
		case <-s.shim.shutdown:
			return nil
		}
	}
}
