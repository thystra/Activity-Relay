package models

import (
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/viper"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

func TestNewRelayConfig(t *testing.T) {
	t.Run("Load valid configuration successfully", func(t *testing.T) {
		relayConfig, err := NewRelayConfig()
		if err != nil {
			t.Fatal(err)
		}

		if relayConfig.serverBind != "0.0.0.0:8080" {
			t.Errorf("Expected RelayConfig.serverBind to be '0.0.0.0:8080', but got '%s'", relayConfig.serverBind)
		}
		if relayConfig.observabilityBind != "" {
			t.Errorf("Expected RelayConfig.observabilityBind to be empty, but got '%s'", relayConfig.observabilityBind)
		}
		if relayConfig.domain.Host != "relay.toot.yukimochi.jp" {
			t.Errorf("Expected RelayConfig.domain.Host to be 'relay.toot.yukimochi.jp', but got '%s'", relayConfig.domain.Host)
		}
		if relayConfig.serviceName != "YUKIMOCHI Toot Relay Service" {
			t.Errorf("Expected RelayConfig.serviceName to be 'YUKIMOCHI Toot Relay Service', but got '%s'", relayConfig.serviceName)
		}
		if relayConfig.serviceSummary != "YUKIMOCHI Toot Relay Service is Running by Activity-Relay" {
			t.Errorf("Expected RelayConfig.serviceSummary to be 'YUKIMOCHI Toot Relay Service is Running by Activity-Relay', but got '%s'", relayConfig.serviceSummary)
		}
		if relayConfig.serviceIconURL.String() != "https://example.com/example_icon.png" {
			t.Errorf("Expected RelayConfig.serviceIconURL to be 'https://example.com/example_icon.png', but got '%s'", relayConfig.serviceIconURL.String())
		}
		if relayConfig.serviceImageURL.String() != "https://example.com/example_image.png" {
			t.Errorf("Expected RelayConfig.serviceImageURL to be 'https://example.com/example_image.png', but got '%s'", relayConfig.serviceImageURL.String())
		}
		if relayConfig.OutboundSignatureProfile() != relayhttpsig.ProfileLegacy {
			t.Errorf(
				"Expected omitted outbound profile to be legacy, got %q",
				relayConfig.OutboundSignatureProfile(),
			)
		}
	})

	t.Run("Fail to load invalid configuration", func(t *testing.T) {
		invalidConfig := map[string]string{
			"ACTOR_PEM@notFound":                 "../misc/test/notfound.pem",
			"ACTOR_PEM@invalidKey":               "../misc/test/actor.dh.pem",
			"REDIS_URL@invalidURL":               "",
			"REDIS_URL@unreachableHost":          "redis://localhost:6380",
			"OBSERVABILITY_BIND@missingPort":     "127.0.0.1",
			"OBSERVABILITY_BIND@zeroPort":        "127.0.0.1:0",
			"OUTBOUND_SIGNATURE_PROFILE@unknown": "automatic",
		}

		for key, value := range invalidConfig {
			viperKey := strings.Split(key, "@")[0]
			valid := viper.GetString(viperKey)

			viper.Set(viperKey, value)
			_, err := NewRelayConfig()
			if err == nil {
				t.Errorf("Expected error for invalid key '%s', but got nil", key)
			}

			viper.Set(viperKey, valid)
		}
	})
}

func createRelayConfig(t *testing.T) *RelayConfig {
	relayConfig, err := NewRelayConfig()
	if err != nil {
		t.Fatal(err)
	}

	return relayConfig
}

func TestRelayConfig_ServerBind(t *testing.T) {
	relayConfig := createRelayConfig(t)
	if relayConfig.ServerBind() != relayConfig.serverBind {
		t.Errorf("Expected ServerBind() to return '%s', but got '%s'", relayConfig.serverBind, relayConfig.ServerBind())
	}
}

func TestRelayConfig_ObservabilityBind(t *testing.T) {
	previous := viper.GetString("OBSERVABILITY_BIND")
	viper.Set("OBSERVABILITY_BIND", "127.0.0.1:9090")
	defer viper.Set("OBSERVABILITY_BIND", previous)

	relayConfig := createRelayConfig(t)
	if relayConfig.ObservabilityBind() != "127.0.0.1:9090" {
		t.Errorf("Expected ObservabilityBind() to return '127.0.0.1:9090', but got '%s'", relayConfig.ObservabilityBind())
	}
}

func TestRelayConfig_ServerHostname(t *testing.T) {
	relayConfig := createRelayConfig(t)
	if relayConfig.ServerHostname() != relayConfig.domain {
		t.Errorf("Expected ServerHostname() to return '%v', but got '%v'", relayConfig.domain, relayConfig.ServerHostname())
	}
}

func TestRelayConfig_DumpWelcomeMessage(t *testing.T) {
	relayConfig := createRelayConfig(t)
	w := relayConfig.DumpWelcomeMessage("Testing", "")

	informations := map[string]string{
		"module NAME":       "Testing",
		"RELAY NAME":        relayConfig.serviceName,
		"RELAY DOMAIN":      relayConfig.domain.Host,
		"REDIS URL":         relayConfig.redisDisplayURL,
		"BIND ADDRESS":      relayConfig.serverBind,
		"OBSERVABILITY":     "disabled",
		"SIGNATURE PROFILE": relayhttpsig.ProfileLegacy.String(),
		"JOB_CONCURRENCY":   strconv.Itoa(relayConfig.jobConcurrency),
	}

	for key, information := range informations {
		if !strings.Contains(w, information) {
			t.Errorf("Expected welcome message to contain '%s' for key '%s', but not found", information, key)
		}
	}
}

func TestNewMachineryServer(t *testing.T) {
	relayConfig := createRelayConfig(t)

	_, err := NewMachineryServer(relayConfig)
	if err != nil {
		t.Errorf("Expected NewMachineryServer to succeed, but got error: %v", err)
	}
}

func TestRelayConfigOutboundSignatureProfile(t *testing.T) {
	previous := viper.Get("OUTBOUND_SIGNATURE_PROFILE")
	defer viper.Set("OUTBOUND_SIGNATURE_PROFILE", previous)

	viper.Set("OUTBOUND_SIGNATURE_PROFILE", "RFC9421")
	relayConfig := createRelayConfig(t)
	if got := relayConfig.OutboundSignatureProfile(); got != relayhttpsig.ProfileRFC9421 {
		t.Fatalf("outbound profile = %q; want rfc9421", got)
	}
}

func TestRelayConfigDualSignatureProfile(t *testing.T) {
	previous := viper.Get("OUTBOUND_SIGNATURE_PROFILE")
	defer viper.Set("OUTBOUND_SIGNATURE_PROFILE", previous)

	viper.Set("OUTBOUND_SIGNATURE_PROFILE", "dual")
	relayConfig := createRelayConfig(t)
	if got := relayConfig.OutboundSignatureProfile(); got != relayhttpsig.ProfileDual {
		t.Fatalf("outbound profile = %q; want dual", got)
	}
	if relayConfig.OutboundSignatureNegotiator() == nil {
		t.Fatal("dual config has no destination negotiator")
	}
}
