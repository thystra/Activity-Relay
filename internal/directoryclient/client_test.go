package directoryclient

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/common-fate/httpsig/alg_rsa"
	"github.com/common-fate/httpsig/contentdigest"
	"github.com/common-fate/httpsig/sigbase"
	"github.com/common-fate/httpsig/sigset"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

const (
	testDirectoryOrigin = "https://directory.example"
	testRelayActor      = "https://relay.example/actor"
	testRelayBase       = "https://relay.example"
	testKeyID           = testRelayActor + "#main-key"
)

var testNow = time.Date(2026, 8, 5, 12, 34, 56, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatalf("read test key: %v", err)
	}
	block, trailing := pem.Decode(encoded)
	if block == nil || len(trailing) != 0 {
		t.Fatal("test key PEM is invalid")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse test key: %v", err)
	}
	return key
}

func newTestClient(
	t *testing.T,
	transport http.RoundTripper,
	nonce func() (string, error),
) *Client {
	t.Helper()
	client, err := New(Options{
		Origin:        testDirectoryOrigin,
		RelayActor:    testRelayActor,
		PublicBaseURL: testRelayBase,
		KeyID:         testKeyID,
		PrivateKey:    testPrivateKey(t),
		HTTPClient:    &http.Client{Transport: transport, Timeout: time.Minute},
		Now:           func() time.Time { return testNow },
		Nonce:         nonce,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client.httpClient.Timeout != defaultRequestTimeout {
		t.Fatalf("client timeout = %s", client.httpClient.Timeout)
	}
	return client
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func successBody(operation Operation, outcome Outcome) string {
	document, _ := json.Marshal(Response{
		ProtocolVersion: ProtocolVersion,
		Operation:       operation,
		Outcome:         outcome,
		RelayActor:      testRelayActor,
	})
	return string(document)
}

func protocolErrorBody(code ErrorCode) string {
	document, _ := json.Marshal(errorResponse{
		ProtocolVersion: ProtocolVersion,
		Error: errorDocument{
			Code:    code,
			Message: "bounded fixture message",
		},
	})
	return string(document)
}

func TestRegisterEmitsExactDirectorySignatureProfile(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte
	client := newTestClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request.Clone(request.Context())
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		capturedBody = body
		return jsonHTTPResponse(http.StatusCreated, successBody(OperationRegister, OutcomeCreated)), nil
	}), func() (string, error) { return "fixture-nonce-0001", nil })

	response, err := client.Register(context.Background())
	if err != nil || response.Outcome != OutcomeCreated {
		t.Fatalf("Register() = (%#v, %v)", response, err)
	}
	if captured == nil || captured.Method != http.MethodPost ||
		captured.URL.String() != testDirectoryOrigin+registerPath ||
		captured.Host != "directory.example" {
		t.Fatalf("captured request = %#v", captured)
	}
	wantBody := `{"protocol_version":1,"operation":"register","relay_actor":"https://relay.example/actor","public_base_url":"https://relay.example"}`
	if string(capturedBody) != wantBody {
		t.Fatalf("body = %q, want %q", capturedBody, wantBody)
	}
	wantDigest, err := relayhttpsig.RFC9530ContentDigestSHA256(capturedBody)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if captured.Header.Get("Content-Type") != "application/json" ||
		captured.Header.Get("Content-Digest") != wantDigest ||
		captured.Header.Get("Date") != testNow.Format(http.TimeFormat) ||
		captured.Header.Get("Digest") != "" {
		t.Fatalf("request headers = %#v", captured.Header)
	}
	fixtureBytes, err := os.ReadFile(filepath.Join(
		"..", "..", "testdata", "directory", "v1",
		"activity-relay-register.valid.json",
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
		t.Fatalf("shared fixture does not match generated request: %#v", fixture)
	}
	set, err := sigset.Unmarshal(captured)
	if err != nil {
		t.Fatalf("parse signature fields: %v", err)
	}
	if len(set.Messages) != 1 || set.Messages[SignatureLabel] == nil {
		t.Fatalf("signature set = %#v", set.Messages)
	}
	message, err := set.Find(SignatureTag)
	if err != nil {
		t.Fatalf("find directory signature: %v", err)
	}
	if message.Input.KeyID != testKeyID || message.Input.Tag != SignatureTag ||
		message.Input.Alg != SignatureAlg || message.Input.Nonce != "fixture-nonce-0001" ||
		!message.Input.Created.Equal(testNow) ||
		!message.Input.Expires.Equal(testNow.Add(SignatureTTL)) ||
		!reflect.DeepEqual(message.Input.CoveredComponents, postComponents) {
		t.Fatalf("signature input = %#v", message.Input)
	}
	captured.Body = io.NopCloser(bytes.NewReader(capturedBody))
	captured.ContentLength = int64(len(capturedBody))
	base, err := sigbase.Derive(message.Input, nil, captured, contentdigest.SHA256)
	if err != nil {
		t.Fatalf("derive signature base: %v", err)
	}
	canonical, err := base.CanonicalString(message.Input)
	if err != nil {
		t.Fatalf("canonical signature base: %v", err)
	}
	if err := alg_rsa.NewRSAPKCS256Verifier(&testPrivateKey(t).PublicKey).Verify(
		context.Background(), canonical, message.Signature,
	); err != nil {
		t.Fatalf("verify directory signature: %v", err)
	}
}

func TestHeartbeatReconcilesExactlyOnceOnlyWhenNotRegistered(t *testing.T) {
	var mu sync.Mutex
	var paths, nonces []string
	nonceCounter := 0
	client := newTestClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		set, err := sigset.Unmarshal(request)
		if err != nil {
			t.Fatalf("parse signature: %v", err)
		}
		message, err := set.Find(SignatureTag)
		if err != nil {
			t.Fatalf("find signature: %v", err)
		}
		mu.Lock()
		paths = append(paths, request.URL.Path)
		nonces = append(nonces, message.Input.Nonce)
		call := len(paths)
		mu.Unlock()
		switch call {
		case 1:
			return jsonHTTPResponse(http.StatusConflict, protocolErrorBody(ErrorRelayNotRegistered)), nil
		case 2:
			return jsonHTTPResponse(http.StatusCreated, successBody(OperationRegister, OutcomeCreated)), nil
		case 3:
			return jsonHTTPResponse(http.StatusOK, successBody(OperationHeartbeat, OutcomeRecorded)), nil
		default:
			t.Fatalf("unexpected request %d", call)
			return nil, nil
		}
	}), func() (string, error) {
		nonceCounter++
		return fmt.Sprintf("nonce-%d", nonceCounter), nil
	})

	response, err := client.HeartbeatWithRegisterReconciliation(context.Background())
	if err != nil || response.Outcome != OutcomeRecorded {
		t.Fatalf("HeartbeatWithRegisterReconciliation() = (%#v, %v)", response, err)
	}
	if !reflect.DeepEqual(paths, []string{heartbeatPath, registerPath, heartbeatPath}) {
		t.Fatalf("paths = %#v", paths)
	}
	if !reflect.DeepEqual(nonces, []string{"nonce-1", "nonce-2", "nonce-3"}) {
		t.Fatalf("nonces = %#v", nonces)
	}
}

func TestHeartbeatDoesNotReconcileOtherErrors(t *testing.T) {
	for _, code := range []ErrorCode{ErrorInvalidRequest, ErrorAuthenticationFailed, ErrorEnrollmentClosed} {
		t.Run(string(code), func(t *testing.T) {
			calls := 0
			client := newTestClient(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				status := http.StatusBadRequest
				if code == ErrorAuthenticationFailed {
					status = http.StatusUnauthorized
				} else if code == ErrorEnrollmentClosed {
					status = http.StatusForbidden
				}
				return jsonHTTPResponse(status, protocolErrorBody(code)), nil
			}), func() (string, error) { return "nonce", nil })
			_, err := client.HeartbeatWithRegisterReconciliation(context.Background())
			var protocolError *ProtocolError
			if !errors.As(err, &protocolError) || protocolError.Code != code || calls != 1 {
				t.Fatalf("result = (%v, calls=%d)", err, calls)
			}
		})
	}
}

func TestClientRejectsMalformedOversizedAndRedirectResponses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header string
		body   string
		want   error
	}{
		{name: "unknown field", status: 200, header: "application/json", body: `{"protocol_version":1,"operation":"heartbeat","outcome":"recorded","relay_actor":"https://relay.example/actor","extra":true}`, want: ErrDirectoryResponse},
		{name: "duplicate field", status: 200, header: "application/json", body: `{"protocol_version":1,"protocol_version":1,"operation":"heartbeat","outcome":"recorded","relay_actor":"https://relay.example/actor"}`, want: ErrDirectoryResponse},
		{name: "unknown error", status: 400, header: "application/json", body: `{"protocol_version":1,"error":{"code":"other","message":"x"}}`, want: ErrDirectoryResponse},
		{name: "wrong media type", status: 200, header: "application/json; charset=utf-8", body: successBody(OperationHeartbeat, OutcomeRecorded), want: ErrDirectoryResponse},
		{name: "oversized", status: 200, header: "application/json", body: strings.Repeat("x", int(maximumResponseBytes)+1), want: ErrResponseTooLarge},
		{name: "redirect", status: 307, header: "application/json", body: protocolErrorBody(ErrorLifecycleUnavailable), want: ErrDirectoryResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := newTestClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				response := jsonHTTPResponse(test.status, test.body)
				response.Header.Set("Content-Type", test.header)
				response.Request = request
				if test.status == http.StatusTemporaryRedirect {
					response.Header.Set("Location", "https://other.example/v1/relays/heartbeat")
				}
				return response, nil
			}), func() (string, error) { return "nonce", nil })
			_, err := client.Heartbeat(context.Background())
			if !errors.Is(err, test.want) || calls != 1 {
				t.Fatalf("Heartbeat() = (%v, calls=%d), want %v", err, calls, test.want)
			}
		})
	}
}

func TestClientTransportErrorsAreRedacted(t *testing.T) {
	client := newTestClient(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return nil, errors.New("private transport detail with attacker response")
	}), func() (string, error) { return "nonce", nil })
	_, err := client.Register(context.Background())
	if !errors.Is(err, ErrDirectoryTransport) || strings.Contains(err.Error(), "private") {
		t.Fatalf("Register() error = %v", err)
	}
}

func TestParseDirectoriesIsCanonicalBoundedAndDefaultOff(t *testing.T) {
	parsed, err := ParseDirectories([]Directory{
		{Origin: "https://directory.example", Enabled: true},
		{Origin: "https://directory2.example:8443"},
	})
	if err != nil || len(parsed) != 2 || !parsed[0].Enabled || parsed[1].Enabled {
		t.Fatalf("ParseDirectories() = (%#v, %v)", parsed, err)
	}
	if empty, err := ParseDirectories(nil); err != nil || len(empty) != 0 {
		t.Fatalf("ParseDirectories(nil) = (%#v, %v)", empty, err)
	}
	invalid := []string{
		"http://directory.example",
		"https://directory.example/",
		"https://DIRECTORY.example",
		"https://directory.example:443",
		"https://user@directory.example",
		"https://directory.example?query=1",
		" https://directory.example",
	}
	for _, value := range invalid {
		if _, err := ParseDirectories([]Directory{{Origin: value}}); !errors.Is(err, ErrDirectoryConfiguration) {
			t.Fatalf("ParseDirectories(%q) error = %v", value, err)
		}
	}
	tooMany := make([]Directory, MaximumDirectories+1)
	for index := range tooMany {
		tooMany[index].Origin = fmt.Sprintf("https://directory-%d.example", index)
	}
	if _, err := ParseDirectories(tooMany); !errors.Is(err, ErrDirectoryConfiguration) {
		t.Fatalf("ParseDirectories(too many) error = %v", err)
	}
	if _, err := ParseDirectories([]Directory{
		{Origin: testDirectoryOrigin}, {Origin: testDirectoryOrigin},
	}); !errors.Is(err, ErrDirectoryConfiguration) {
		t.Fatalf("ParseDirectories(duplicate) error = %v", err)
	}
}

func TestDecodeStrictJSONRejectsNestedDuplicate(t *testing.T) {
	body := []byte(`{"protocol_version":1,"error":{"code":"internal_error","code":"internal_error","message":"x"}}`)
	if !duplicateJSONName(body) {
		t.Fatal("nested duplicate was not detected")
	}
	var document errorResponse
	if !errors.Is(decodeStrictJSON(body, &document), ErrDirectoryResponse) {
		t.Fatal("nested duplicate decoded")
	}
}

func TestContentDigestIsOverExactBody(t *testing.T) {
	body := []byte(`{"exact":"bytes"}`)
	digest, err := relayhttpsig.RFC9530ContentDigestSHA256(body)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	if err := relayhttpsig.VerifyRFC9530ContentDigestSHA256([]string{digest}, body); err != nil {
		t.Fatalf("verify digest: %v", err)
	}
	if bytes.Equal(body, append(body, '\n')) {
		t.Fatal("test body mutation guard failed")
	}
}

func TestStatusIsStrictBoundedAndUnsigned(t *testing.T) {
	client := newTestClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet || request.URL.Path != statusPath ||
			request.Header.Get("Signature") != "" || request.Header.Get("Signature-Input") != "" {
			t.Fatalf("status request = %#v", request)
		}
		return jsonHTTPResponse(http.StatusOK, `{"schema_version":2,"service":"activity-relay-directory","version":"test","public_base_url":"https://directory.example","lifecycle_enabled":true,"lifecycle_available":true,"enrollment_open":false}`), nil
	}), func() (string, error) { return "unused", nil })

	status, err := client.Status(context.Background())
	if err != nil || status.Version != "test" || !status.LifecycleAvailable || status.EnrollmentOpen {
		t.Fatalf("Status() = (%#v, %v)", status, err)
	}
}

func TestProtocolErrorCarriesOnlyBoundedRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		want  time.Duration
		err   error
	}{
		{name: "absent"},
		{name: "seconds", value: "7", want: 7 * time.Second},
		{name: "bounded", value: "999999", want: MaximumRetryAfter},
		{name: "invalid", value: "tomorrow", err: ErrDirectoryResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				response := jsonHTTPResponse(http.StatusTooManyRequests, protocolErrorBody(ErrorRateLimited))
				if test.value != "" {
					response.Header.Set("Retry-After", test.value)
				}
				return response, nil
			}), func() (string, error) { return "nonce", nil })
			_, err := client.Register(context.Background())
			if test.err != nil {
				if !errors.Is(err, test.err) {
					t.Fatalf("Register() error = %v", err)
				}
				return
			}
			var protocolError *ProtocolError
			if !errors.As(err, &protocolError) || protocolError.RetryAfter != test.want ||
				protocolError.Code != ErrorRateLimited {
				t.Fatalf("Register() error = %#v", err)
			}
		})
	}
}
