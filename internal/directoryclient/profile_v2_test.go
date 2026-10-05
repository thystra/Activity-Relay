package directoryclient

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeRelayProfileAndDigestAreDeterministic(t *testing.T) {
	profile, err := NormalizeRelayProfile(RelayProfile{
		Languages:  []string{"fr", "en", "en"},
		Notes:      " note ",
		ContactURL: "https://relay.example/contact",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(profile.Languages, []string{"en", "fr"}) || profile.Notes != "note" {
		t.Fatalf("profile = %#v", profile)
	}
	client := &Client{profile: profile}
	first := client.ProfileDigest()
	second := client.ProfileDigest()
	if len(first) != 64 || first != second {
		t.Fatalf("digests = %q %q", first, second)
	}
}

func TestStatusLifecycleVersionValidation(t *testing.T) {
	for _, test := range []struct {
		status Status
		valid  bool
	}{
		{Status{SchemaVersion: 2}, true},
		{Status{SchemaVersion: 3}, true},
		{Status{SchemaVersion: 4, LifecycleProtocolVersions: []int{1}}, true},
		{Status{SchemaVersion: 4, LifecycleProtocolVersions: []int{1, 2}}, true},
		{Status{SchemaVersion: 4}, false},
		{Status{SchemaVersion: 4, LifecycleProtocolVersions: []int{2}}, false},
		{Status{SchemaVersion: 4, LifecycleProtocolVersions: []int{1, 1}}, false},
		{Status{SchemaVersion: 3, LifecycleProtocolVersions: []int{1}}, false},
	} {
		if got := validStatusLifecycleVersions(test.status); got != test.valid {
			t.Fatalf("status %#v valid=%t want %t", test.status, got, test.valid)
		}
	}
}

func TestV2RegisterMatchesSharedFixture(t *testing.T) {
	fixtureNow := time.Date(2026, 10, 3, 22, 30, 0, 0, time.UTC)
	profile := RelayProfile{
		ParticipationMode: "open",
		Availability:      "public",
		RelayType:         "general",
		Languages:         []string{"en", "fr"},
		Countries:         []string{"US"},
		Regions:           []string{"North America"},
		Topics:            []string{"general"},
		ContactFediverse:  "@relay@example.social",
		ContactEmail:      "relay@example.com",
		ContactURL:        "https://relay.example/contact",
		ParticipationURL:  "https://relay.example/join",
		Notes:             "Public community relay",
	}
	var captured *http.Request
	var capturedBody []byte
	client, err := New(Options{
		Origin:        testDirectoryOrigin,
		RelayActor:    testRelayActor,
		PublicBaseURL: testRelayBase,
		Profile:       profile,
		KeyID:         testKeyID,
		PrivateKey:    testPrivateKey(t),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			captured = request.Clone(request.Context())
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			capturedBody = body
			return jsonHTTPResponse(
				http.StatusCreated,
				`{"protocol_version":2,"operation":"register","outcome":"created","relay_actor":"https://relay.example/actor"}`,
			), nil
		})},
		Now:   func() time.Time { return fixtureNow },
		Nonce: func() (string, error) { return "fixture-v2-nonce-0001", nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response, err := client.registerVersion(context.Background(), ProtocolVersion2)
	if err != nil || response.ProtocolVersion != ProtocolVersion2 || response.Outcome != OutcomeCreated {
		t.Fatalf("v2 register = (%#v, %v)", response, err)
	}
	if captured == nil || captured.Method != http.MethodPost ||
		captured.URL.String() != testDirectoryOrigin+registerPathV2 || captured.Host != "directory.example" {
		t.Fatalf("captured request = %#v", captured)
	}

	fixtureBytes, err := os.ReadFile(filepath.Join(
		"..", "..", "testdata", "directory", "v2", "activity-relay-register.valid.json",
	))
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	var fixture struct {
		Method         string `json:"method"`
		Scheme         string `json:"scheme"`
		Authority      string `json:"authority"`
		Target         string `json:"target"`
		ContentType    string `json:"content_type"`
		ContentDigest  string `json:"content_digest"`
		Date           string `json:"date"`
		Body           string `json:"body"`
		SignatureInput string `json:"signature_input"`
		Signature      string `json:"signature"`
		KeyID          string `json:"key_id"`
		KeyOwner       string `json:"key_owner"`
		KeyActor       string `json:"key_actor"`
		PublicKeyPEM   string `json:"public_key_pem"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatalf("decode shared fixture: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&testPrivateKey(t).PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	if fixture.Method != captured.Method || fixture.Scheme != captured.URL.Scheme ||
		fixture.Authority != captured.Host || fixture.Target != captured.URL.RequestURI() ||
		fixture.ContentType != captured.Header.Get("Content-Type") ||
		fixture.ContentDigest != captured.Header.Get("Content-Digest") ||
		fixture.Date != captured.Header.Get("Date") || fixture.Body != string(capturedBody) ||
		fixture.SignatureInput != captured.Header.Get("Signature-Input") ||
		fixture.Signature != captured.Header.Get("Signature") || fixture.KeyID != testKeyID ||
		fixture.KeyOwner != testRelayActor || fixture.KeyActor != testRelayActor ||
		fixture.PublicKeyPEM != publicPEM {
		t.Fatalf("shared v2 fixture does not match generated request: %#v", fixture)
	}
}

func TestV3RegisterMatchesSharedFixture(t *testing.T) {
	fixtureNow := time.Date(2026, 10, 3, 22, 30, 0, 0, time.UTC)
	profile := RelayProfile{
		ParticipationMode: "open",
		Availability:      "public",
		RelayType:         "general",
		Languages:         []string{"en", "fr"},
		Countries:         []string{"US"},
		Regions:           []string{"North America"},
		Topics:            []string{"general"},
		ContactFediverse:  "@relay@example.social",
		ContactEmail:      "relay@example.com",
		ContactURL:        "https://relay.example/contact",
		ParticipationURL:  "https://relay.example/join",
		Notes:             "Public community relay",
	}
	var captured *http.Request
	var capturedBody []byte
	client, err := New(Options{
		Origin:        testDirectoryOrigin,
		RelayActor:    testRelayActor,
		PublicBaseURL: testRelayBase,
		Profile:       profile,
		KeyID:         testKeyID,
		PrivateKey:    testPrivateKey(t),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			captured = request.Clone(request.Context())
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			capturedBody = body
			return jsonHTTPResponse(
				http.StatusCreated,
				`{"protocol_version":2,"operation":"register","outcome":"created","relay_actor":"https://relay.example/actor"}`,
			), nil
		})},
		Now:       func() time.Time { return fixtureNow },
		Nonce:     func() (string, error) { return "fixture-v3-nonce-0001", nil },
		Telemetry: func(context.Context) (Telemetry, error) { return Telemetry{ParticipatingInstanceCount: 12}, nil },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response, err := client.registerVersion(context.Background(), ProtocolVersion3)
	if err != nil || response.ProtocolVersion != ProtocolVersion3 || response.Outcome != OutcomeCreated {
		t.Fatalf("v3 register = (%#v, %v)", response, err)
	}
	if captured == nil || captured.Method != http.MethodPost ||
		captured.URL.String() != testDirectoryOrigin+registerPathV3 || captured.Host != "directory.example" {
		t.Fatalf("captured request = %#v", captured)
	}

	fixtureBytes, err := os.ReadFile(filepath.Join(
		"..", "..", "testdata", "directory", "v3", "activity-relay-register.valid.json",
	))
	if err != nil {
		t.Fatalf("read shared fixture: %v", err)
	}
	var fixture struct {
		Method         string `json:"method"`
		Scheme         string `json:"scheme"`
		Authority      string `json:"authority"`
		Target         string `json:"target"`
		ContentType    string `json:"content_type"`
		ContentDigest  string `json:"content_digest"`
		Date           string `json:"date"`
		Body           string `json:"body"`
		SignatureInput string `json:"signature_input"`
		Signature      string `json:"signature"`
		KeyID          string `json:"key_id"`
		KeyOwner       string `json:"key_owner"`
		KeyActor       string `json:"key_actor"`
		PublicKeyPEM   string `json:"public_key_pem"`
	}
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatalf("decode shared fixture: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&testPrivateKey(t).PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	if fixture.Method != captured.Method || fixture.Scheme != captured.URL.Scheme ||
		fixture.Authority != captured.Host || fixture.Target != captured.URL.RequestURI() ||
		fixture.ContentType != captured.Header.Get("Content-Type") ||
		fixture.ContentDigest != captured.Header.Get("Content-Digest") ||
		fixture.Date != captured.Header.Get("Date") || fixture.Body != string(capturedBody) ||
		fixture.SignatureInput != captured.Header.Get("Signature-Input") ||
		fixture.Signature != captured.Header.Get("Signature") || fixture.KeyID != testKeyID ||
		fixture.KeyOwner != testRelayActor || fixture.KeyActor != testRelayActor ||
		fixture.PublicKeyPEM != publicPEM {
		t.Fatalf("shared v3 fixture does not match generated request: %#v", fixture)
	}
}
