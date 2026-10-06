package directorycommand

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryconfig"
	"github.com/thystra/Activity-Relay/internal/directoryscheduler"
)

func TestScheduledManualHeartbeatCoordinatesAndAdvancesState(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	completedAt := time.Date(2026, 10, 6, 4, 15, 0, 0, time.UTC)
	store := &commandTestStore{
		state: directoryscheduler.State{
			Registered:             true,
			LastSuccess:            completedAt.Add(-4 * time.Hour),
			NextAttempt:            completedAt.Add(20 * time.Hour),
			LastOutcome:            "heartbeat",
			Diagnostic:             "none",
			LastObserved:           completedAt.Add(-4 * time.Hour),
			ProfileDigest:          strings.Repeat("a", 64),
			ProfileProtocolVersion: 1,
		},
		lease:    &commandTestLease{},
		acquired: true,
	}
	deps := testDependencies(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if store.lease.released {
			t.Fatal("scheduler lease was released before manual heartbeat completed")
		}
		if request.Method == http.MethodGet && request.URL.Path == "/v1/status" {
			return statusResponse(), nil
		}
		if request.Method != http.MethodPost || request.URL.Path != "/v1/relays/heartbeat" {
			t.Fatalf("heartbeat request = %s %s", request.Method, request.URL.Path)
		}
		return jsonResponse(http.StatusOK, successResponse("heartbeat", "recorded")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }
	deps.now = func() time.Time { return completedAt }

	stdout, _, err := executeCommand(t, deps, path, "directory", "heartbeat", commandTestOrigin)
	if err != nil || !strings.Contains(stdout, "recorded") || !store.lease.released {
		t.Fatalf("result=(%q,%v) released=%t", stdout, err, store.lease.released)
	}
	wantNext := directoryscheduler.NextHeartbeatAttempt(commandTestActor, commandTestOrigin, completedAt)
	if !store.state.Registered || store.state.LastSuccess != completedAt ||
		store.state.LastObserved != completedAt || store.state.NextAttempt != wantNext ||
		store.state.LastOutcome != "heartbeat" || store.state.Diagnostic != "none" || store.state.Attempt != 0 {
		t.Fatalf("state=%#v wantNext=%s", store.state, wantNext)
	}
}

func TestScheduledManualSyncPersistsProfileProtocolState(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	completedAt := time.Date(2026, 10, 6, 4, 30, 0, 0, time.UTC)
	store := &commandTestStore{lease: &commandTestLease{}, acquired: true}
	deps := testDependencies(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.Path == "/v1/status" {
			return statusResponse(), nil
		}
		if request.Method != http.MethodPost || request.URL.Path != "/v1/relays/register" {
			t.Fatalf("sync request = %s %s", request.Method, request.URL.Path)
		}
		return jsonResponse(http.StatusCreated, successResponse("register", "created")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }
	deps.now = func() time.Time { return completedAt }

	if _, _, err := executeCommand(t, deps, path, "directory", "sync", commandTestOrigin); err != nil {
		t.Fatal(err)
	}
	if len(store.state.ProfileDigest) != 64 || store.state.ProfileProtocolVersion != 1 ||
		store.state.LastOutcome != "registered" || !store.state.Registered {
		t.Fatalf("state=%#v", store.state)
	}
}

func TestScheduledManualLifecycleRequiresLeaseBeforeRemoteTraffic(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	store := &commandTestStore{lease: &commandTestLease{}, acquired: false}
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not run")
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }

	_, _, err := executeCommand(t, deps, path, "directory", "heartbeat", commandTestOrigin)
	if err == nil || !strings.Contains(err.Error(), "lease is unavailable") || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}
