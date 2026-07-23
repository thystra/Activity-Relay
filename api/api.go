package api

import (
	"net/http"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
	"github.com/yukimochi/Activity-Relay/models"
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

	ActorCache      *cache.Cache
	MachineryServer *machinery.Server
	RelayState      models.RelayState
)

func Entrypoint(g *models.RelayConfig, v string) error {
	var err error

	version = v
	GlobalConfig = g

	err = initialize(GlobalConfig)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	handlersRegister(mux)

	logrus.Info("Starting API Server at ", GlobalConfig.ServerBind())
	server := &http.Server{
		Addr:              GlobalConfig.ServerBind(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 * 1024,
	}
	err = server.ListenAndServe()
	if err != nil {
		return err
	}

	return nil
}

func initialize(globalConfig *models.RelayConfig) error {
	var err error

	redisClient := globalConfig.RedisClient()
	RelayState = models.NewState(redisClient, true)
	RelayState.ListenNotify(nil)

	MachineryServer, err = models.NewMachineryServer(globalConfig)
	if err != nil {
		return err
	}

	RelayActor = models.NewActivityPubActorFromRelayConfig(globalConfig)
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
	mux.HandleFunc("/inbox", func(w http.ResponseWriter, r *http.Request) {
		handleInbox(w, r, decodeActivity)
	})
}
