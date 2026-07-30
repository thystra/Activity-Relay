package models

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/yukimochi/machinery-v1/v1"
	"github.com/yukimochi/machinery-v1/v1/config"
)

// RelayConfig contains valid configuration.
type RelayConfig struct {
	actorKey          *rsa.PrivateKey
	domain            *url.URL
	redisClient       *redis.Client
	redisURL          string
	serverBind        string
	observabilityBind string
	serviceName       string
	serviceSummary    string
	serviceIconURL    *url.URL
	serviceImageURL   *url.URL
	jobConcurrency    int
	maxActivityBytes  int64
	maxFanoutTargets  int
	maxQueueJobs      int64
}

// NewRelayConfig create valid RelayConfig from viper configuration.
func NewRelayConfig() (*RelayConfig, error) {
	domain, err := url.ParseRequestURI("https://" + viper.GetString("RELAY_DOMAIN"))
	if err != nil {
		return nil, errors.New("RELAY_DOMAIN: " + err.Error())
	}

	iconURL, err := url.ParseRequestURI(viper.GetString("RELAY_ICON"))
	if err != nil {
		logrus.Warn("RELAY_ICON: INVALID OR EMPTY. THIS COLUMN IS DISABLED.")
		iconURL = nil
	}

	imageURL, err := url.ParseRequestURI(viper.GetString("RELAY_IMAGE"))
	if err != nil {
		logrus.Warn("RELAY_IMAGE: INVALID OR EMPTY. THIS COLUMN IS DISABLED.")
		imageURL = nil
	}

	jobConcurrency := viper.GetInt("JOB_CONCURRENCY")
	if jobConcurrency < 1 {
		return nil, errors.New("JOB_CONCURRENCY IS 0 OR EMPTY. SHOULD BE SET MORE THAN 1")
	}
	maxActivityBytes := viper.GetInt64("MAX_ACTIVITY_BYTES")
	if maxActivityBytes == 0 {
		maxActivityBytes = 1024 * 1024
	}
	if maxActivityBytes < 1024 {
		return nil, errors.New("MAX_ACTIVITY_BYTES SHOULD BE AT LEAST 1024")
	}
	maxFanoutTargets := viper.GetInt("MAX_FANOUT_TARGETS")
	if maxFanoutTargets == 0 {
		maxFanoutTargets = 5000
	}
	if maxFanoutTargets < 1 {
		return nil, errors.New("MAX_FANOUT_TARGETS SHOULD BE POSITIVE")
	}
	maxQueueJobs := viper.GetInt64("MAX_QUEUE_JOBS")
	if maxQueueJobs == 0 {
		maxQueueJobs = 100000
	}
	if maxQueueJobs < 1 {
		return nil, errors.New("MAX_QUEUE_JOBS SHOULD BE POSITIVE")
	}

	privateKey, err := readPrivateKeyRSA(viper.GetString("ACTOR_PEM"))
	if err != nil {
		return nil, errors.New("ACTOR_PEM: " + err.Error())
	}

	redisURL := viper.GetString("REDIS_URL")
	redisOption, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, errors.New("REDIS_URL: " + err.Error())
	}
	redisClient := redis.NewClient(redisOption)
	err = redisClient.Ping(context.TODO()).Err()
	if err != nil {
		return nil, errors.New("REDIS_URL: " + err.Error())
	}

	serverBind := viper.GetString("RELAY_BIND")
	observabilityBind := strings.TrimSpace(viper.GetString("OBSERVABILITY_BIND"))
	if observabilityBind != "" {
		if err := validateBindAddress(observabilityBind); err != nil {
			return nil, errors.New("OBSERVABILITY_BIND: " + err.Error())
		}
	}

	return &RelayConfig{
		actorKey:          privateKey,
		domain:            domain,
		redisClient:       redisClient,
		redisURL:          redisURL,
		serverBind:        serverBind,
		observabilityBind: observabilityBind,
		serviceName:       viper.GetString("RELAY_SERVICENAME"),
		serviceSummary:    viper.GetString("RELAY_SUMMARY"),
		serviceIconURL:    iconURL,
		serviceImageURL:   imageURL,
		jobConcurrency:    jobConcurrency,
		maxActivityBytes:  maxActivityBytes,
		maxFanoutTargets:  maxFanoutTargets,
		maxQueueJobs:      maxQueueJobs,
	}, nil
}

// ServerBind is API Server's bind interface definition.
func (relayConfig *RelayConfig) ServerBind() string {
	return relayConfig.serverBind
}

// ObservabilityBind is the optional private metrics and probe listener.
func (relayConfig *RelayConfig) ObservabilityBind() string {
	return relayConfig.observabilityBind
}

// ServerHostname is API Server's hostname definition.
func (relayConfig *RelayConfig) ServerHostname() *url.URL {
	return relayConfig.domain
}

// ServerServiceName is API Server's servername definition.
func (relayConfig *RelayConfig) ServerServiceName() string {
	return relayConfig.serviceName
}

// JobConcurrency is API Worker's jobConcurrency definition.
func (relayConfig *RelayConfig) JobConcurrency() int {
	return relayConfig.jobConcurrency
}

// MaxActivityBytes limits an inbound ActivityPub request body.
func (relayConfig *RelayConfig) MaxActivityBytes() int64 { return relayConfig.maxActivityBytes }

// MaxFanoutTargets limits jobs created by one inbound activity.
func (relayConfig *RelayConfig) MaxFanoutTargets() int { return relayConfig.maxFanoutTargets }

// MaxQueueJobs limits admission based on the Redis broker backlog.
func (relayConfig *RelayConfig) MaxQueueJobs() int64 { return relayConfig.maxQueueJobs }

// ActorKey is API Worker's HTTPSignature private key.
func (relayConfig *RelayConfig) ActorKey() *rsa.PrivateKey {
	return relayConfig.actorKey
}

// RedisClient is return redis client from RelayConfig.
func (relayConfig *RelayConfig) RedisClient() *redis.Client {
	return relayConfig.redisClient
}

// DumpWelcomeMessage provide build and config information string.
func (relayConfig *RelayConfig) DumpWelcomeMessage(moduleName string, version string) string {
	observabilityBind := relayConfig.observabilityBind
	if observabilityBind == "" {
		observabilityBind = "disabled"
	}
	return fmt.Sprintf(`Welcome to Activity-Relay %s - %s
 - Configuration
RELAY NAME      : %s
RELAY DOMAIN    : %s
REDIS URL       : %s
BIND ADDRESS    : %s
OBSERVABILITY   : %s
JOB_CONCURRENCY : %s
`, version, moduleName, relayConfig.serviceName, relayConfig.domain.Host, relayConfig.redisURL, relayConfig.serverBind, observabilityBind, strconv.Itoa(relayConfig.jobConcurrency))
}

func validateBindAddress(address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if port == "" {
		return errors.New("port is empty")
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return errors.New("port must be numeric")
	}
	if portNumber == 0 {
		return errors.New("port must be greater than zero")
	}
	return nil
}

// NewMachineryServer create Redis backed Machinery Server from RelayConfig.
func NewMachineryServer(globalConfig *RelayConfig) (*machinery.Server, error) {
	cnf := &config.Config{
		Broker:          globalConfig.redisURL,
		DefaultQueue:    "relay",
		ResultBackend:   globalConfig.redisURL,
		ResultsExpireIn: 1,
	}
	newServer, err := machinery.NewServer(cnf)

	return newServer, err
}
