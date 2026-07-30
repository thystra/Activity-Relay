package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/httpsignature"
	"github.com/thystra/Activity-Relay/internal/observability"
	"github.com/thystra/Activity-Relay/models"
	"github.com/yukimochi/machinery-v1/v1"
)

var (
	version      string
	GlobalConfig *models.RelayConfig
	// RelayActor : Relay's Actor
	RelayActor models.Actor
	// Nodeinfo : Relay's Nodeinfo
	Nodeinfo models.NodeinfoResources
	// WebfingerResources : Relay's Webfinger Resources
	WebfingerResources []models.WebfingerResource

	ActorCache          *cache.Cache
	MachineryServer     *machinery.Server
	RemoteRequestSigner *httpsignature.Signer
	RelayState          models.RelayState
	OperationalMetrics  *observability.Recorder
)

type httpServerBinding struct {
	name     string
	server   *http.Server
	listener net.Listener
}

func Entrypoint(g *models.RelayConfig, v string) error {
	version = v
	GlobalConfig = g
	if err := initialize(GlobalConfig); err != nil {
		return err
	}

	mux := http.NewServeMux()
	handlersRegister(mux)
	publicHandler := http.Handler(mux)

	var observabilityService *observability.Service
	if GlobalConfig.ObservabilityBind() != "" {
		observabilityService = observability.New(
			version,
			GlobalConfig.RedisClient(),
			relayRuntimeSnapshot,
		)
		publicHandler = observabilityService.Instrument(publicHandler)
	}

	apiListener, err := net.Listen("tcp", GlobalConfig.ServerBind())
	if err != nil {
		return fmt.Errorf("listen for API server on %s: %w", GlobalConfig.ServerBind(), err)
	}
	bindings := []httpServerBinding{
		{
			name:     "API Server",
			server:   newHTTPServer(GlobalConfig.ServerBind(), publicHandler),
			listener: apiListener,
		},
	}

	if observabilityService != nil {
		observabilityBind := GlobalConfig.ObservabilityBind()
		observabilityListener, err := net.Listen("tcp", observabilityBind)
		if err != nil {
			_ = apiListener.Close()
			return fmt.Errorf("listen for observability server on %s: %w", observabilityBind, err)
		}
		bindings = append(bindings, httpServerBinding{
			name:     "Observability Server",
			server:   newHTTPServer(observabilityBind, observabilityService.Handler()),
			listener: observabilityListener,
		})
	}

	return serveHTTPServers(bindings)
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
}

func serveHTTPServers(bindings []httpServerBinding) error {
	errorsChannel := make(chan error, len(bindings))
	for _, binding := range bindings {
		binding := binding
		logrus.Info("Starting ", binding.name, " at ", binding.listener.Addr())
		go func() {
			err := binding.server.Serve(binding.listener)
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			} else if err != nil {
				err = fmt.Errorf("%s: %w", binding.name, err)
			}
			errorsChannel <- err
		}()
	}

	err := <-errorsChannel
	for _, binding := range bindings {
		_ = binding.server.Close()
	}
	return err
}

func initialize(globalConfig *models.RelayConfig) error {
	var err error
	redisClient := globalConfig.RedisClient()
	OperationalMetrics = nil
	if globalConfig.ObservabilityBind() != "" {
		OperationalMetrics = observability.NewRecorder(redisClient)
	}
	RelayState = models.NewState(redisClient, true)
	RelayState.ListenNotify(nil)

	MachineryServer, err = models.NewMachineryServer(globalConfig)
	if err != nil {
		return err
	}

	RelayActor = models.NewActivityPubActorFromRelayConfig(globalConfig)
	RemoteRequestSigner, err = httpsignature.NewSigner(
		RelayActor.PublicKey.ID,
		globalConfig.ActorKey(),
	)
	if err != nil {
		return err
	}
	ActorCache = cache.New(5*time.Minute, 10*time.Minute)
	Nodeinfo = models.GenerateNodeinfoResources(globalConfig.ServerHostname(), version)
	WebfingerResources = append(WebfingerResources, RelayActor.GenerateWebfingerResource(globalConfig.ServerHostname()))

	return nil
}
func handlersRegister(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/nodeinfo", handleNodeinfoLink)
	mux.HandleFunc("/.well-known/webfinger", handleWebfinger)
	mux.HandleFunc("/nodeinfo/2.1", handleNodeinfo)
	mux.HandleFunc("/status.json", handleRelayStatus)
	mux.HandleFunc("/actor", handleRelayActor)
	mux.HandleFunc("/actor/outbox", handleRelayOutbox)
	mux.HandleFunc("/actor/followers", handleRelayFollowers)
	mux.HandleFunc("/actor/following", handleRelayFollowing)
	mux.HandleFunc("/inbox", func(w http.ResponseWriter, r *http.Request) {
		handleInbox(w, r, decodeActivity)
	})
}
