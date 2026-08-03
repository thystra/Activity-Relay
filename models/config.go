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
	"time"

	machinery "github.com/RichardKnop/machinery/v2"
	redisbackend "github.com/RichardKnop/machinery/v2/backends/redis"
	redisbroker "github.com/RichardKnop/machinery/v2/brokers/redis"
	machineryconfig "github.com/RichardKnop/machinery/v2/config"
	eagerlock "github.com/RichardKnop/machinery/v2/locks/eager"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

// RelayConfig contains valid configuration.
type RelayConfig struct {
	actorKey                        *rsa.PrivateKey
	domain                          *url.URL
	redisClient                     *redis.Client
	redisOptions                    *redis.Options
	redisURL                        string
	redisDisplayURL                 string
	serverBind                      string
	observabilityBind               string
	serviceName                     string
	serviceSummary                  string
	serviceIconURL                  *url.URL
	serviceImageURL                 *url.URL
	jobConcurrency                  int
	maxActivityBytes                int64
	maxFanoutTargets                int
	maxQueueJobs                    int64
	publicAddressDistributionPolicy PublicAddressDistributionPolicy
	outboundSignatureProfile        relayhttpsig.Profile
	outboundSignatureNegotiator     *relayhttpsig.DestinationNegotiator
}

// NewRelayConfig create valid RelayConfig from viper configuration.
func NewRelayConfig() (*RelayConfig, error) {
	publicAddressDistributionPolicy, err := ParsePublicAddressDistributionPolicy(
		viper.GetString("PUBLIC_ADDRESS_DISTRIBUTION_POLICY"),
	)
	if err != nil {
		return nil, errors.New(
			"PUBLIC_ADDRESS_DISTRIBUTION_POLICY: " + err.Error(),
		)
	}

	outboundSignatureProfile, err := relayhttpsig.ParseOutboundProfile(
		viper.GetString("OUTBOUND_SIGNATURE_PROFILE"),
	)
	if err != nil {
		return nil, errors.New(
			"OUTBOUND_SIGNATURE_PROFILE: " + err.Error(),
		)
	}
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
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, errors.New("REDIS_URL: " + err.Error())
	}
	redisClient := redis.NewClient(redisOptions)
	pingContext, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelPing()
	if err := redisClient.Ping(pingContext).Err(); err != nil {
		_ = redisClient.Close()
		return nil, errors.New("REDIS_URL: " + err.Error())
	}
	capabilityStore, err :=
		relayhttpsig.NewRedisDestinationCapabilityStore(
			redisClient,
			"",
		)
	if err != nil {
		_ = redisClient.Close()
		return nil, errors.New(
			"HTTP SIGNATURE CAPABILITY STORE: " + err.Error(),
		)
	}
	outboundSignatureNegotiator, err :=
		relayhttpsig.NewDestinationNegotiator(
			relayhttpsig.DestinationNegotiatorOptions{
				Store: capabilityStore,
			},
		)
	if err != nil {
		_ = redisClient.Close()
		return nil, errors.New(
			"HTTP SIGNATURE NEGOTIATOR: " + err.Error(),
		)
	}

	serverBind := viper.GetString("RELAY_BIND")
	observabilityBind := strings.TrimSpace(viper.GetString("OBSERVABILITY_BIND"))
	if observabilityBind != "" {
		if err := validateBindAddress(observabilityBind); err != nil {
			return nil, errors.New("OBSERVABILITY_BIND: " + err.Error())
		}
	}

	return &RelayConfig{
		actorKey:                        privateKey,
		domain:                          domain,
		redisClient:                     redisClient,
		redisOptions:                    redisOptions,
		redisURL:                        redisURL,
		redisDisplayURL:                 redactedRedisURL(redisURL),
		serverBind:                      serverBind,
		observabilityBind:               observabilityBind,
		serviceName:                     viper.GetString("RELAY_SERVICENAME"),
		serviceSummary:                  viper.GetString("RELAY_SUMMARY"),
		serviceIconURL:                  iconURL,
		serviceImageURL:                 imageURL,
		jobConcurrency:                  jobConcurrency,
		maxActivityBytes:                maxActivityBytes,
		maxFanoutTargets:                maxFanoutTargets,
		maxQueueJobs:                    maxQueueJobs,
		publicAddressDistributionPolicy: publicAddressDistributionPolicy,
		outboundSignatureProfile:        outboundSignatureProfile,
		outboundSignatureNegotiator:     outboundSignatureNegotiator,
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

// PublicAddressDistributionPolicy returns the effective public-address
// fan-out policy.
func (relayConfig *RelayConfig) PublicAddressDistributionPolicy() PublicAddressDistributionPolicy {
	if relayConfig == nil || relayConfig.publicAddressDistributionPolicy == "" {
		return PublicAddressPublicAndUnlisted
	}
	return relayConfig.publicAddressDistributionPolicy
}

// OutboundSignatureProfile is the process-wide authorized-fetch and
// delivery HTTP-signature profile.
func (relayConfig *RelayConfig) OutboundSignatureProfile() relayhttpsig.Profile {
	if relayConfig == nil || relayConfig.outboundSignatureProfile == "" {
		return relayhttpsig.ProfileLegacy
	}
	return relayConfig.outboundSignatureProfile
}

// OutboundSignatureNegotiator returns the shared destination-capability
// planner constructed from the validated Redis connection.
func (relayConfig *RelayConfig) OutboundSignatureNegotiator() *relayhttpsig.DestinationNegotiator {
	if relayConfig == nil {
		return nil
	}
	return relayConfig.outboundSignatureNegotiator
}

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
SIGNATURE PROFILE: %s
PUBLIC ADDRESS POLICY: %s
JOB_CONCURRENCY : %s
`, version, moduleName, relayConfig.serviceName, relayConfig.domain.Host, relayConfig.redisDisplayURL, relayConfig.serverBind, observabilityBind, relayConfig.OutboundSignatureProfile(), relayConfig.PublicAddressDistributionPolicy(), strconv.Itoa(relayConfig.jobConcurrency))
}

func redactedRedisURL(redisURL string) string {
	parsed, err := url.Parse(redisURL)
	if err != nil {
		return "<invalid>"
	}
	return parsed.Redacted()
}

func machineryRedisAddress(options *redis.Options) string {
	if options.Username == "" && options.Password == "" {
		return options.Addr
	}
	if options.Username == "" {
		return options.Password + "@" + options.Addr
	}
	return options.Username + ":" + options.Password + "@" + options.Addr
}

func machineryConfig(options *redis.Options, displayURL string) *machineryconfig.Config {
	var tlsConfig = options.TLSConfig
	if tlsConfig != nil {
		tlsConfig = tlsConfig.Clone()
	}
	return &machineryconfig.Config{
		Broker:          displayURL,
		DefaultQueue:    machineryQueueName,
		ResultBackend:   displayURL,
		ResultsExpireIn: machineryResultsExpireSeconds,
		TLSConfig:       tlsConfig,
		Redis: &machineryconfig.RedisConfig{
			MaxIdle:                3,
			IdleTimeout:            240,
			ReadTimeout:            15,
			WriteTimeout:           15,
			ConnectTimeout:         15,
			NormalTasksPollPeriod:  1000,
			DelayedTasksPollPeriod: 500,
		},
	}
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

const machineryQueueName = "relay"
const machineryResultsExpireSeconds = 1

// NewMachineryServer creates the Redis-backed Machinery v2 server while
// preserving the established relay queue, delayed-task key, and result format.
func NewMachineryServer(globalConfig *RelayConfig) (*machinery.Server, error) {
	if globalConfig == nil || globalConfig.redisOptions == nil {
		return nil, errors.New("Redis configuration is unavailable")
	}

	options := globalConfig.redisOptions
	cnf := machineryConfig(options, globalConfig.redisDisplayURL)

	switch options.Network {
	case "tcp":
		address := machineryRedisAddress(options)
		broker := redisbroker.NewGR(cnf, []string{address}, options.DB)
		backend := redisbackend.NewGR(cnf, []string{address}, options.DB)
		return machinery.NewServer(cnf, broker, backend, eagerlock.New()), nil
	case "unix":
		broker := redisbroker.New(
			cnf,
			"",
			options.Username,
			options.Password,
			options.Addr,
			options.DB,
		)
		backend := redisbackend.New(
			cnf,
			"",
			options.Username,
			options.Password,
			options.Addr,
			options.DB,
		)
		return machinery.NewServer(cnf, broker, backend, eagerlock.New()), nil
	default:
		return nil, fmt.Errorf(
			"unsupported Redis network %q for Machinery",
			options.Network,
		)
	}
}
