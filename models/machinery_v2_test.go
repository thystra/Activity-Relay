package models

import (
	"crypto/tls"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestRedactedRedisURL(t *testing.T) {
	const raw = "rediss://relay:secret@example.test:6380/6"
	got := redactedRedisURL(raw)

	if strings.Contains(got, "secret") {
		t.Fatalf("redacted Redis URL exposes password: %q", got)
	}
	if !strings.Contains(got, "relay:xxxxx@") {
		t.Fatalf("redacted Redis URL does not preserve a redacted userinfo marker: %q", got)
	}
}

func TestNewMachineryServerV2TCP(t *testing.T) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: "redis.example.test",
	}
	relayConfig := &RelayConfig{
		redisOptions: &redis.Options{
			Network:   "tcp",
			Addr:      "redis.example.test:6380",
			Username:  "relay",
			Password:  "secret",
			DB:        6,
			TLSConfig: tlsConfig,
		},
		redisURL:        "rediss://relay:secret@redis.example.test:6380/6",
		redisDisplayURL: "rediss://relay:xxxxx@redis.example.test:6380/6",
	}

	server, err := NewMachineryServer(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	if server == nil || server.GetBroker() == nil || server.GetBackend() == nil {
		t.Fatal("Machinery v2 server is incomplete")
	}

	config := server.GetConfig()
	if config.DefaultQueue != machineryQueueName {
		t.Errorf("DefaultQueue = %q; want %q", config.DefaultQueue, machineryQueueName)
	}
	if config.ResultsExpireIn != machineryResultsExpireSeconds {
		t.Errorf(
			"ResultsExpireIn = %d; want %d",
			config.ResultsExpireIn,
			machineryResultsExpireSeconds,
		)
	}
	if strings.Contains(config.Broker, "secret") ||
		strings.Contains(config.ResultBackend, "secret") {
		t.Fatal("Machinery configuration exposes the Redis password")
	}
	if config.TLSConfig == nil {
		t.Fatal("Machinery TLS configuration is missing")
	}
	if config.TLSConfig == tlsConfig {
		t.Fatal("Machinery TLS configuration was not cloned")
	}
	if config.TLSConfig.ServerName != "redis.example.test" {
		t.Errorf("TLS ServerName = %q", config.TLSConfig.ServerName)
	}
}

func TestNewMachineryServerV2UnixSocket(t *testing.T) {
	relayConfig := &RelayConfig{
		redisOptions: &redis.Options{
			Network:  "unix",
			Addr:     "/run/redis/activity-relay.sock",
			Username: "relay",
			Password: "secret",
			DB:       2,
		},
		redisURL:        "unix://relay:secret@/run/redis/activity-relay.sock?db=2",
		redisDisplayURL: "unix://relay:xxxxx@/run/redis/activity-relay.sock?db=2",
	}

	server, err := NewMachineryServer(relayConfig)
	if err != nil {
		t.Fatal(err)
	}
	if server == nil || server.GetBroker() == nil || server.GetBackend() == nil {
		t.Fatal("Unix-socket Machinery v2 server is incomplete")
	}
	if server.GetConfig().DefaultQueue != machineryQueueName {
		t.Errorf(
			"DefaultQueue = %q; want %q",
			server.GetConfig().DefaultQueue,
			machineryQueueName,
		)
	}
}

func TestNewMachineryServerRejectsUnsupportedNetwork(t *testing.T) {
	relayConfig := &RelayConfig{
		redisOptions: &redis.Options{
			Network: "udp",
			Addr:    "127.0.0.1:6379",
		},
		redisDisplayURL: "redis://127.0.0.1:6379",
	}

	_, err := NewMachineryServer(relayConfig)
	if err == nil || !strings.Contains(err.Error(), "unsupported Redis network") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewMachineryServerRejectsMissingConfiguration(t *testing.T) {
	if _, err := NewMachineryServer(nil); err == nil {
		t.Fatal("expected missing Redis configuration to fail")
	}
}
