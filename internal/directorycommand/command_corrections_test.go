package directorycommand

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
	"github.com/thystra/Activity-Relay/internal/directoryscheduler"
)

func (store *commandTestStore) SaveOwned(
	_ context.Context,
	_ string,
	lease directoryscheduler.Lease,
	state directoryscheduler.State,
) (bool, error) {
	if !store.acquired || lease != store.lease {
		return false, nil
	}
	store.state = state
	return true, nil
}

func TestUnregisterCoordinatesWhenRuntimeGateWasDisabled(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.Replace(string(body), "DIRECTORY_SCHEDULER_ENABLED: true", "DIRECTORY_SCHEDULER_ENABLED: false", 1))
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	store := &commandTestStore{
		state:    directoryscheduler.State{Registered: true, LastOutcome: "heartbeat", Diagnostic: "none"},
		lease:    &commandTestLease{},
		acquired: true,
	}
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		if store.state.LastOutcome != "disabled" || !store.state.Registered {
			t.Fatalf("scheduler state was not suppressed before request: %#v", store.state)
		}
		return jsonResponse(http.StatusUnauthorized, errorResponse("authentication_failed")), nil
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(config directoryconfig.Config) (directoryscheduler.StateStore, error) {
		if config.SchedulerEnabled || config.RedisURL == "" {
			t.Fatalf("reloaded configuration = %#v", config)
		}
		return store, nil
	}

	_, _, err = executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	if err == nil || calls != 1 || !store.lease.released || !store.state.Registered {
		t.Fatalf("error=%v calls=%d lease=%#v state=%#v", err, calls, store.lease, store.state)
	}
}

type failingLoadStore struct {
	lease *commandTestLease
}

func (*failingLoadStore) Load(context.Context, string) (directoryscheduler.State, error) {
	return directoryscheduler.State{}, directoryscheduler.ErrStore
}
func (*failingLoadStore) Save(context.Context, string, directoryscheduler.State) error {
	return errors.New("must not save")
}
func (*failingLoadStore) SaveOwned(context.Context, string, directoryscheduler.Lease, directoryscheduler.State) (bool, error) {
	return false, errors.New("must not save")
}
func (store *failingLoadStore) Acquire(context.Context, string, time.Duration) (directoryscheduler.Lease, bool, error) {
	return store.lease, true, nil
}

func TestUnregisterFailsClosedOnMalformedSchedulerState(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	store := &failingLoadStore{lease: &commandTestLease{}}
	calls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not run")
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }

	_, _, err := executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	config, loadErr := directoryconfig.Load(path)
	entry, entryErr := config.Directory(commandTestOrigin)
	if err == nil || !strings.Contains(err.Error(), "state is invalid") || calls != 0 ||
		loadErr != nil || entryErr != nil || !entry.Enabled || !store.lease.released {
		t.Fatalf("error=%v calls=%d entry=%#v load=%v entryErr=%v released=%t", err, calls, entry, loadErr, entryErr, store.lease.released)
	}
}

func TestManualCommandDoesNotSleepPastLongRetryAfter(t *testing.T) {
	path := testCommandConfig(t, true)
	calls := 0
	var sleeps []time.Duration
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		response := jsonResponse(http.StatusTooManyRequests, errorResponse("rate_limited"))
		response.Header.Set("Retry-After", "7200")
		return response, nil
	}), func() (string, error) { return "nonce", nil }, &sleeps)

	_, _, err := executeCommand(t, deps, path, "directory", "register", commandTestOrigin)
	if err == nil || calls != 1 || len(sleeps) != 0 {
		t.Fatalf("error=%v calls=%d sleeps=%#v", err, calls, sleeps)
	}
	var protocolError *directoryclient.ProtocolError
	if errors.As(err, &protocolError) {
		t.Fatalf("command error unexpectedly exposed protocol type: %#v", protocolError)
	}
}

type coordinationStore struct {
	mu    sync.Mutex
	state directoryscheduler.State
	held  bool
	token int
}

func (store *coordinationStore) Load(context.Context, string) (directoryscheduler.State, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.state, nil
}
func (store *coordinationStore) Save(_ context.Context, _ string, state directoryscheduler.State) error {
	store.mu.Lock()
	store.state = state
	store.mu.Unlock()
	return nil
}
func (store *coordinationStore) SaveOwned(
	_ context.Context,
	_ string,
	lease directoryscheduler.Lease,
	state directoryscheduler.State,
) (bool, error) {
	owned, ok := lease.(*coordinationLease)
	store.mu.Lock()
	defer store.mu.Unlock()
	if !ok || !store.held || owned.store != store || owned.token != store.token {
		return false, nil
	}
	store.state = state
	return true, nil
}
func (store *coordinationStore) Acquire(context.Context, string, time.Duration) (directoryscheduler.Lease, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.held {
		return nil, false, nil
	}
	store.held = true
	store.token++
	return &coordinationLease{store: store, token: store.token}, true, nil
}
func (store *coordinationStore) isHeld() bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.held
}

type coordinationLease struct {
	store *coordinationStore
	token int
}

func (lease *coordinationLease) Renew(context.Context, time.Duration) (bool, error) {
	lease.store.mu.Lock()
	defer lease.store.mu.Unlock()
	return lease.store.held && lease.store.token == lease.token, nil
}
func (lease *coordinationLease) Release(context.Context) error {
	lease.store.mu.Lock()
	defer lease.store.mu.Unlock()
	if lease.store.held && lease.store.token == lease.token {
		lease.store.held = false
	}
	return nil
}

type blockingSchedulerClient struct {
	started chan struct{}
	finish  chan struct{}
}

func (client *blockingSchedulerClient) Register(ctx context.Context) (directoryclient.Response, error) {
	close(client.started)
	select {
	case <-ctx.Done():
		return directoryclient.Response{}, ctx.Err()
	case <-client.finish:
		return directoryclient.Response{
			Operation: directoryclient.OperationRegister,
			Outcome:   directoryclient.OutcomeCreated,
		}, nil
	}
}
func (client *blockingSchedulerClient) HeartbeatWithRegisterReconciliation(context.Context) (directoryclient.Response, error) {
	return directoryclient.Response{}, errors.New("unexpected heartbeat")
}

func TestRuntimeGateDisableCannotRaceUnregisterPastActiveSchedulerLease(t *testing.T) {
	path := testCommandConfig(t, true)
	enableTestScheduler(t, path)
	store := &coordinationStore{}
	blocking := &blockingSchedulerClient{started: make(chan struct{}), finish: make(chan struct{})}
	scheduler, err := directoryscheduler.New(directoryscheduler.Config{
		RelayActor: commandTestActor,
		Directories: []directoryclient.Directory{
			{Origin: commandTestOrigin, Enabled: true},
		},
		Store: store,
		Enabled: func(origin string) (bool, error) {
			config, err := directoryconfig.Load(path)
			if err != nil || !config.SchedulerEnabled {
				return false, err
			}
			entry, err := config.Directory(origin)
			return err == nil && entry.Enabled, err
		},
		Clients: func(directoryclient.Directory) (directoryscheduler.Client, error) {
			return blocking, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	schedulerDone := make(chan error, 1)
	go func() { schedulerDone <- scheduler.RunOnce(context.Background()) }()
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("scheduler request did not start")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.Replace(string(body), "DIRECTORY_SCHEDULER_ENABLED: true", "DIRECTORY_SCHEDULER_ENABLED: false", 1))
	if err := os.WriteFile(path, body, 0o640); err != nil {
		t.Fatal(err)
	}
	remoteCalls := 0
	deps := testDependencies(t, roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		remoteCalls++
		return nil, errors.New("must not run")
	}), func() (string, error) { return "nonce", nil }, nil)
	deps.store = func(directoryconfig.Config) (directoryscheduler.StateStore, error) { return store, nil }

	_, _, unregisterErr := executeCommand(t, deps, path, "directory", "unregister", commandTestOrigin)
	config, loadErr := directoryconfig.Load(path)
	entry, entryErr := config.Directory(commandTestOrigin)
	if unregisterErr == nil || !strings.Contains(unregisterErr.Error(), "lease is unavailable") ||
		remoteCalls != 0 || loadErr != nil || entryErr != nil || !entry.Enabled {
		t.Fatalf("unregister=%v calls=%d entry=%#v load=%v entryErr=%v", unregisterErr, remoteCalls, entry, loadErr, entryErr)
	}

	close(blocking.finish)
	select {
	case err := <-schedulerDone:
		if err != nil {
			t.Fatalf("scheduler completion error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not release its lease")
	}
	if store.isHeld() {
		t.Fatal("scheduler lease remained held")
	}
}
