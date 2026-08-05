package directorycommand

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
	"github.com/thystra/Activity-Relay/internal/directoryscheduler"
)

const (
	commandTestOrigin = "https://directory.example"
	commandTestActor  = "https://relay.example/actor"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func testCommandKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, trailing := pem.Decode(body)
	if block == nil || len(trailing) != 0 {
		t.Fatal("invalid test key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return key, path
}

func testCommandConfig(t *testing.T, enabled bool) string {
	t.Helper()
	_, keyPath := testCommandKey(t)
	path := filepath.Join(t.TempDir(), "config.yml")
	body := fmt.Sprintf(
		"# retain me\nACTOR_PEM: %s\nRELAY_DOMAIN: relay.example\nOTHER: keep\nDIRECTORIES:\n  - origin: %s\n    enabled: %t\n",
		keyPath,
		commandTestOrigin,
		enabled,
	)
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func enableTestScheduler(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = append([]byte("DIRECTORY_SCHEDULER_ENABLED: true\nREDIS_URL: redis://127.0.0.1:6379/0\n"), body...)
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
}

type commandTestLease struct {
	released bool
}

func (*commandTestLease) Renew(context.Context, time.Duration) (bool, error) { return true, nil }
func (lease *commandTestLease) Release(context.Context) error {
	lease.released = true
	return nil
}

type commandTestStore struct {
	state    directoryscheduler.State
	lease    *commandTestLease
	acquired bool
}

func (store *commandTestStore) Load(context.Context, string) (directoryscheduler.State, error) {
	return store.state, nil
}
func (store *commandTestStore) Save(_ context.Context, _ string, state directoryscheduler.State) error {
	store.state = state
	return nil
}
func (store *commandTestStore) Acquire(context.Context, string, time.Duration) (directoryscheduler.Lease, bool, error) {
	if !store.acquired {
		return nil, false, nil
	}
	return store.lease, true, nil
}

func testDependencies(
	t *testing.T,
	transport http.RoundTripper,
	nonce func() (string, error),
	sleeps *[]time.Duration,
) dependencies {
	t.Helper()
	deps := productionDependencies()
	deps.client = func(config directoryconfig.Config, origin string) (*directoryclient.Client, error) {
		return directoryclient.New(directoryclient.Options{
			Origin:        origin,
			RelayActor:    config.RelayActor,
			PublicBaseURL: config.PublicBaseURL,
			KeyID:         config.KeyID,
			PrivateKey:    config.PrivateKey,
			HTTPClient:    &http.Client{Transport: transport},
			Now:           func() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC) },
			Nonce:         nonce,
		})
	}
	deps.sleep = func(_ context.Context, delay time.Duration) error {
		if sleeps != nil {
			*sleeps = append(*sleeps, delay)
		}
		return nil
	}
	return deps
}

func executeCommand(t *testing.T, deps dependencies, configPath string, args ...string) (string, string, error) {
	t.Helper()
	root := &cobra.Command{Use: "relay", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("config", configPath, "configuration path")
	root.AddCommand(buildCommand(deps))
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func successResponse(operation, outcome string) string {
	return fmt.Sprintf(
		`{"protocol_version":1,"operation":%q,"outcome":%q,"relay_actor":%q}`,
		operation,
		outcome,
		commandTestActor,
	)
}

func errorResponse(code string) string {
	return fmt.Sprintf(`{"protocol_version":1,"error":{"code":%q,"message":"bounded"}}`, code)
}

func TestRegisterRetriesTransportWithFreshNonce(t *testing.T) {
	path := testCommandConfig(t, true)
	calls := 0
	var signatures []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		signatures = append(signatures, request.Header.Get("Signature-Input"))
		if calls == 1 {
			return nil, errors.New("private transport detail")
		}
		return jsonResponse(http.StatusCreated, successResponse("register", "created")), nil
	})
	nonceCounter := 0
	var sleeps []time.Duration
	deps := testDependencies(t, transport, func() (string, error) {
		nonceCounter++
		return fmt.Sprintf("nonce-%d", nonceCounter), nil
	}, &sleeps)

	stdout, _, err := executeCommand(t, deps, path, "directory", "register", commandTestOrigin)
	if err != nil || !strings.Contains(stdout, "created") || calls != 2 ||
		len(signatures) != 2 || signatures[0] == signatures[1] ||
		!strings.Contains(signatures[0], "nonce-1") || !strings.Contains(signatures[1], "nonce-2") ||
		len(sleeps) != 1 || sleeps[0] != initialBackoff {
		t.Fatalf("result=(%q, %v), calls=%d signatures=%#v sleeps=%#v", stdout, err, calls, signatures, sleeps)
	}
}

func TestAuthenticationAndPolicyErrorsAreNotRetried(t *testing.T) {
	for _, test := range []struct {
		code   string
		status int
	}{
		{code: "authentication_failed", status: http.StatusUnauthorized},
		{code: "enrollment_closed", status: http.StatusForbidden},
	} {
		t.Run(test.code, func(t *testing.T) {
			path := testCommandConfig(t, true)
			calls := 0
			deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				return jsonResponse(test.status, errorResponse(test.code)), nil
			}), func() (string, error) { return "nonce", nil }, nil)
			_, _, err := executeCommand(t, deps, path, "directory", "register", commandTestOrigin)
			if err == nil || calls != 1 || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestRateLimitHonorsBoundedRetryAfter(t *testing.T) {
	path := testCommandConfig(t, true)
	calls := 0
	var sleeps []time.Duration
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			response := jsonResponse(http.StatusTooManyRequests, errorResponse("rate_limited"))
			response.Header.Set("Retry-After", "9")
			return response, nil
		}
		return jsonResponse(http.StatusCreated, successResponse("register", "created")), nil
	}), func() (string, error) { return fmt.Sprintf("nonce-%d", calls+1), nil }, &sleeps)
	if _, _, err := executeCommand(t, deps, path, "directory", "register", commandTestOrigin); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(sleeps) != 1 || sleeps[0] != 9*time.Second {
		t.Fatalf("calls=%d sleeps=%#v", calls, sleeps)
	}
}

func TestStatusHeartbeatAndSyncCommandPaths(t *testing.T) {
	path := testCommandConfig(t, true)
	t.Run("local status", func(t *testing.T) {
		deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			t.Fatal("local status made a network request")
			return nil, nil
		}), func() (string, error) { return "nonce", nil }, nil)
		stdout, _, err := executeCommand(t, deps, path, "directory", "status")
		if err != nil || !strings.Contains(stdout, commandTestOrigin+" configured") {
			t.Fatalf("local status = (%q, %v)", stdout, err)
		}
	})

	t.Run("remote status", func(t *testing.T) {
		deps := testDependencies(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet || request.URL.Path != "/v1/status" {
				t.Fatalf("status request = %s %s", request.Method, request.URL.Path)
			}
			return jsonResponse(http.StatusOK, `{"schema_version":2,"service":"activity-relay-directory","version":"test","public_base_url":"https://directory.example","lifecycle_enabled":true,"lifecycle_available":true,"enrollment_open":false}`), nil
		}), func() (string, error) { return "nonce", nil }, nil)
		stdout, _, err := executeCommand(t, deps, path, "directory", "status", commandTestOrigin)
		if err != nil || !strings.Contains(stdout, "lifecycle_available=true") {
			t.Fatalf("remote status = (%q, %v)", stdout, err)
		}
	})

	t.Run("heartbeat", func(t *testing.T) {
		deps := testDependencies(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path != "/v1/relays/heartbeat" {
				t.Fatalf("heartbeat path = %s", request.URL.Path)
			}
			return jsonResponse(http.StatusOK, successResponse("heartbeat", "recorded")), nil
		}), func() (string, error) { return "nonce", nil }, nil)
		stdout, _, err := executeCommand(t, deps, path, "directory", "heartbeat", commandTestOrigin)
		if err != nil || !strings.Contains(stdout, "recorded") {
			t.Fatalf("heartbeat = (%q, %v)", stdout, err)
		}
	})

	t.Run("sync reconciliation", func(t *testing.T) {
		calls := 0
		var signatureInputs []string
		deps := testDependencies(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			signatureInputs = append(signatureInputs, request.Header.Get("Signature-Input"))
			switch calls {
			case 1:
				return jsonResponse(http.StatusConflict, errorResponse("relay_not_registered")), nil
			case 2:
				return jsonResponse(http.StatusCreated, successResponse("register", "created")), nil
			default:
				return jsonResponse(http.StatusOK, successResponse("heartbeat", "recorded")), nil
			}
		}), func() (string, error) {
			return fmt.Sprintf("sync-nonce-%d", calls+1), nil
		}, nil)
		stdout, _, err := executeCommand(t, deps, path, "directory", "sync", commandTestOrigin)
		if err != nil || calls != 3 || !strings.Contains(stdout, "recorded") ||
			len(signatureInputs) != 3 || signatureInputs[0] == signatureInputs[1] ||
			signatureInputs[1] == signatureInputs[2] {
			t.Fatalf("sync=(%q, %v), calls=%d signatures=%#v", stdout, err, calls, signatureInputs)
		}
	})
}

func TestDisabledEntryCannotRegisterHeartbeatOrSync(t *testing.T) {
	for _, operation := range []string{"register", "heartbeat", "sync"} {
		t.Run(operation, func(t *testing.T) {
			path := testCommandConfig(t, false)
			calls := 0
			deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("must not run")
			}), func() (string, error) { return "nonce", nil }, nil)
			_, _, err := executeCommand(t, deps, path, "directory", operation, commandTestOrigin)
			if err == nil || !strings.Contains(err.Error(), "disabled") || calls != 0 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestUnregisterDisablesBeforeRequestAndStaysDisabledOnFailure(t *testing.T) {
	path := testCommandConfig(t, true)
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		config, err := directoryconfig.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := config.Directory(commandTestOrigin)
		if err != nil || entry.Enabled {
			t.Fatalf("entry was not disabled before request: %#v %v", entry, err)
		}
		return jsonResponse(http.StatusUnauthorized, errorResponse("authentication_failed")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	_, stderr, err := executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	if err == nil || calls != 1 || !strings.Contains(stderr, "remains disabled") {
		t.Fatalf("error=%v calls=%d stderr=%q", err, calls, stderr)
	}
	config, loadErr := directoryconfig.Load(path)
	entry, entryErr := config.Directory(commandTestOrigin)
	if loadErr != nil || entryErr != nil || entry.Enabled {
		t.Fatalf("post-failure entry = (%#v, %v, %v)", entry, loadErr, entryErr)
	}
}

func TestUnregisterMayRemoveOnlyAfterRemoteSuccess(t *testing.T) {
	path := testCommandConfig(t, true)
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, successResponse("unregister", "removed")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	stdout, _, err := executeCommand(
		t, deps, path, "directory", "unregister", commandTestOrigin, "--remove",
	)
	if err != nil || !strings.Contains(stdout, "removed "+commandTestOrigin+" from configuration") {
		t.Fatalf("result=(%q, %v)", stdout, err)
	}
	config, err := directoryconfig.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Directory(commandTestOrigin); !errors.Is(err, directoryconfig.ErrNotFound) {
		t.Fatalf("Directory() error = %v", err)
	}
}

func TestScheduledUnregisterLeasesThenSuppressesBeforeRemoteRequest(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	store := &commandTestStore{
		state:    directoryscheduler.State{Registered: true, LastOutcome: "heartbeat", Diagnostic: "none"},
		lease:    &commandTestLease{},
		acquired: true,
	}
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		config, err := directoryconfig.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		entry, err := config.Directory(commandTestOrigin)
		if err != nil || entry.Enabled || store.state.LastOutcome != "disabled" || !store.state.Registered {
			t.Fatalf("remote request preceded durable suppression: entry=%#v state=%#v err=%v", entry, store.state, err)
		}
		return jsonResponse(http.StatusUnauthorized, errorResponse("authentication_failed")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }

	_, _, err := executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	if err == nil || calls != 1 || !store.lease.released || !store.state.Registered {
		t.Fatalf("error=%v calls=%d lease=%#v state=%#v", err, calls, store.lease, store.state)
	}
	stdout, _, statusErr := executeCommand(t, deps, path, "directory", "status")
	if statusErr != nil || !strings.Contains(stdout, commandTestOrigin+" unregister-pending") {
		t.Fatalf("status=(%q, %v)", stdout, statusErr)
	}
}

func TestScheduledUnregisterRequiresLeaseBeforeMutation(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	store := &commandTestStore{lease: &commandTestLease{}, acquired: false}
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not run")
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }

	_, _, err := executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	config, loadErr := directoryconfig.Load(path)
	entry, entryErr := config.Directory(commandTestOrigin)
	if err == nil || !strings.Contains(err.Error(), "lease") || calls != 0 ||
		loadErr != nil || entryErr != nil || !entry.Enabled {
		t.Fatalf("error=%v calls=%d entry=%#v load=%v entryErr=%v", err, calls, entry, loadErr, entryErr)
	}
}

func TestEnvironmentUnregisterRequiresExplicitAcknowledgement(t *testing.T) {
	_, keyPath := testCommandKey(t)
	t.Setenv("ACTOR_PEM", keyPath)
	t.Setenv("RELAY_DOMAIN", "relay.example")
	t.Setenv("DIRECTORIES", `[{origin: "https://directory.example", enabled: true}]`)
	path := filepath.Join(t.TempDir(), "missing.yml")
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return jsonResponse(http.StatusOK, successResponse("unregister", "removed")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	_, _, err := executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	if err == nil || calls != 0 || !strings.Contains(err.Error(), environmentAcknowledgementFlag) {
		t.Fatalf("without acknowledgement: error=%v calls=%d", err, calls)
	}
	_, stderr, err := executeCommand(
		t, deps, path, "directory", "unregister", commandTestOrigin,
		"--"+environmentAcknowledgementFlag,
	)
	if err != nil || calls != 1 || !strings.Contains(stderr, "external configuration source") {
		t.Fatalf("with acknowledgement: error=%v calls=%d stderr=%q", err, calls, stderr)
	}
}

func TestOriginCompletionIsSortedAndDisablesFileCompletion(t *testing.T) {
	path := testCommandConfig(t, true)
	deps := productionDependencies()
	root := &cobra.Command{Use: "relay"}
	root.PersistentFlags().String("config", path, "configuration path")
	directory := buildCommand(deps)
	root.AddCommand(directory)
	register, _, err := directory.Find([]string{"register"})
	if err != nil || register.ValidArgsFunction == nil {
		t.Fatalf("register command = (%#v, %v)", register, err)
	}
	values, directive := register.ValidArgsFunction(register, nil, "https://directory")
	if len(values) != 1 || values[0] != commandTestOrigin ||
		directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("completion = (%#v, %v)", values, directive)
	}
}
