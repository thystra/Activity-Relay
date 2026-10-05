package directoryclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestDirectoryStatusVersionTokenRejectsTerminalControls(t *testing.T) {
	for _, value := range []string{"", "1.3.0-rc3\nFAKE", "\x1b[2J", strings.Repeat("x", 65), "version with spaces"} {
		if validDirectoryVersionToken(value) {
			t.Fatalf("validDirectoryVersionToken(%q) = true", value)
		}
	}
	for _, value := range []string{"1.3.0-rc3", "v1.3.0_rc3+build.1", "devel"} {
		if !validDirectoryVersionToken(value) {
			t.Fatalf("validDirectoryVersionToken(%q) = false", value)
		}
	}
}

func TestDirectoryStatusLifecycleVersionsAreBounded(t *testing.T) {
	versions := make([]int, MaximumLifecycleProtocolVersions+1)
	for i := range versions {
		versions[i] = i + 1
	}
	if validStatusLifecycleVersions(Status{SchemaVersion: 4, LifecycleProtocolVersions: versions}) {
		t.Fatal("oversized lifecycle version list accepted")
	}
}

func TestDirectoryDestinationGuardRejectsSpecialAddressesButAllowsPrivate(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "::1", "0.0.0.0", "169.254.1.2", "fe80::1", "224.0.0.1", "ff02::1"} {
		if !unsafeDirectoryDestination(netip.MustParseAddr(value)) {
			t.Fatalf("%s was not rejected", value)
		}
	}
	for _, value := range []string{"10.0.0.5", "192.168.1.10", "172.16.4.5", "2001:db8::1", "203.0.113.10"} {
		if unsafeDirectoryDestination(netip.MustParseAddr(value)) {
			t.Fatalf("%s was unexpectedly rejected", value)
		}
	}
}

func TestDefaultDirectoryTransportIsBoundedAndDoesNotUseEnvironmentProxy(t *testing.T) {
	client := newDirectoryHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	if transport.Proxy != nil || !transport.DisableCompression || transport.MaxResponseHeaderBytes != maximumResponseHeaderBytes ||
		transport.MaxConnsPerHost != 2 || transport.MaxIdleConnsPerHost != 1 {
		t.Fatalf("transport hardening = %#v", transport)
	}
}

func TestV2RegisterAndHeartbeatRemainTelemetryFree(t *testing.T) {
	var bodies [][]byte
	client, err := New(Options{
		Origin:        testDirectoryOrigin,
		RelayActor:    testRelayActor,
		PublicBaseURL: testRelayBase,
		Profile:       RelayProfile{ParticipationMode: ParticipationOpen},
		KeyID:         testKeyID,
		PrivateKey:    testPrivateKey(t),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			bodies = append(bodies, body)
			operation := OperationRegister
			outcome := OutcomeCreated
			if request.URL.Path == heartbeatPathV2 {
				operation, outcome = OperationHeartbeat, OutcomeRecorded
			}
			response, _ := json.Marshal(Response{ProtocolVersion: ProtocolVersion2, Operation: operation, Outcome: outcome, RelayActor: testRelayActor})
			return jsonHTTPResponse(http.StatusOK, string(response)), nil
		})},
		Now:       func() time.Time { return testNow },
		Nonce:     func() (string, error) { return "telemetry-test-nonce", nil },
		Telemetry: func(context.Context) (Telemetry, error) { return Telemetry{ParticipatingInstanceCount: 12}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.registerVersion(context.Background(), ProtocolVersion2); err != nil {
		t.Fatal(err)
	}
	if _, err := client.heartbeatVersion(context.Background(), ProtocolVersion2); err != nil {
		t.Fatal(err)
	}
	for _, body := range bodies {
		if strings.Contains(string(body), `"telemetry"`) {
			t.Fatalf("v2 unexpectedly carried telemetry: %s", body)
		}
	}
}

func TestV3RegisterAndHeartbeatIncludeBoundedParticipatingTelemetry(t *testing.T) {
	var bodies [][]byte
	client, err := New(Options{
		Origin: testDirectoryOrigin, RelayActor: testRelayActor, PublicBaseURL: testRelayBase,
		Profile: RelayProfile{ParticipationMode: ParticipationOpen}, KeyID: testKeyID, PrivateKey: testPrivateKey(t),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			bodies = append(bodies, body)
			operation, outcome := OperationRegister, OutcomeCreated
			if request.URL.Path == heartbeatPathV3 {
				operation, outcome = OperationHeartbeat, OutcomeRecorded
			}
			response, _ := json.Marshal(Response{ProtocolVersion: ProtocolVersion3, Operation: operation, Outcome: outcome, RelayActor: testRelayActor})
			return jsonHTTPResponse(http.StatusOK, string(response)), nil
		})},
		Now: func() time.Time { return testNow }, Nonce: func() (string, error) { return "telemetry-test-nonce-v3", nil },
		Telemetry: func(context.Context) (Telemetry, error) { return Telemetry{ParticipatingInstanceCount: 12}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.registerVersion(context.Background(), ProtocolVersion3); err != nil {
		t.Fatal(err)
	}
	if _, err := client.heartbeatVersion(context.Background(), ProtocolVersion3); err != nil {
		t.Fatal(err)
	}
	for _, body := range bodies {
		if !strings.Contains(string(body), `"telemetry":{"participating_instance_count":12}`) {
			t.Fatalf("v3 telemetry missing from %s", body)
		}
	}
}
