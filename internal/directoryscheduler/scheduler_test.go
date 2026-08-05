package directoryscheduler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/models"
)

const (
	testActor  = "https://relay.example/actor"
	testOrigin = "https://directory.example"
)

type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	after chan time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) After(time.Duration) <-chan time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.after == nil {
		clock.after = make(chan time.Time)
	}
	return clock.after
}

func (clock *fakeClock) set(now time.Time) {
	clock.mu.Lock()
	clock.now = now
	clock.mu.Unlock()
}

type fakeLease struct {
	renew    bool
	lost     chan time.Time
	released atomic.Bool
}

func (lease *fakeLease) Renew(context.Context, time.Duration) (bool, error) {
	return lease.renew, nil
}
func (lease *fakeLease) Release(context.Context) error {
	lease.released.Store(true)
	return nil
}

type fakeStore struct {
	mu      sync.Mutex
	states  map[string]State
	held    bool
	contend bool
	renew   bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{states: make(map[string]State), renew: true}
}

func (store *fakeStore) Load(_ context.Context, origin string) (State, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.states[origin], nil
}

func (store *fakeStore) Save(_ context.Context, origin string, state State) error {
	store.mu.Lock()
	store.states[origin] = state
	store.mu.Unlock()
	return nil
}

func (store *fakeStore) stateCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.states)
}

func (store *fakeStore) Acquire(context.Context, string, time.Duration) (Lease, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.contend || store.held {
		return nil, false, nil
	}
	store.held = true
	lease := &fakeLease{renew: store.renew}
	return &releasingFakeLease{fakeLease: lease, store: store}, true, nil
}

type releasingFakeLease struct {
	*fakeLease
	store *fakeStore
}

func (lease *releasingFakeLease) Release(ctx context.Context) error {
	lease.store.mu.Lock()
	lease.store.held = false
	lease.store.mu.Unlock()
	return lease.fakeLease.Release(ctx)
}

type fakeClient struct {
	mu             sync.Mutex
	registerCalls  int
	heartbeatCalls int
	err            error
	waitForCancel  bool
}

type schedulerRoundTripFunc func(*http.Request) (*http.Response, error)

func (function schedulerRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func (client *fakeClient) Register(ctx context.Context) (directoryclient.Response, error) {
	client.mu.Lock()
	client.registerCalls++
	wait := client.waitForCancel
	err := client.err
	client.mu.Unlock()
	if wait {
		<-ctx.Done()
		return directoryclient.Response{}, ctx.Err()
	}
	return directoryclient.Response{
		Operation: directoryclient.OperationRegister,
		Outcome:   directoryclient.OutcomeCreated,
	}, err
}

func (client *fakeClient) HeartbeatWithRegisterReconciliation(ctx context.Context) (directoryclient.Response, error) {
	client.mu.Lock()
	client.heartbeatCalls++
	wait := client.waitForCancel
	err := client.err
	client.mu.Unlock()
	if wait {
		<-ctx.Done()
		return directoryclient.Response{}, ctx.Err()
	}
	return directoryclient.Response{
		Operation: directoryclient.OperationHeartbeat,
		Outcome:   directoryclient.OutcomeRecorded,
	}, err
}

func (client *fakeClient) calls() (int, int) {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.registerCalls, client.heartbeatCalls
}

func testScheduler(t *testing.T, store StateStore, clock Clock, client Client, enabled Enabled) *Scheduler {
	t.Helper()
	if enabled == nil {
		enabled = func(string) (bool, error) { return true, nil }
	}
	scheduler, err := New(Config{
		RelayActor:  testActor,
		Directories: []directoryclient.Directory{{Origin: testOrigin, Enabled: true}},
		Store:       store,
		Enabled:     enabled,
		Clients:     func(directoryclient.Directory) (Client, error) { return client, nil },
		Clock:       clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func TestStartupRegisterDailyHeartbeatRestartAndClockRegression(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	client := &fakeClient{}
	scheduler := testScheduler(t, store, clock, client, nil)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := store.states[testOrigin]
	minimum := now.Add(NominalHeartbeatInterval)
	maximum := minimum.Add(MaximumStableJitter)
	if !state.Registered || state.LastOutcome != "registered" ||
		state.NextAttempt.Before(minimum) || state.NextAttempt.After(maximum) ||
		client.registerCalls != 1 {
		t.Fatalf("state=%#v client=%#v", state, client)
	}

	// Restart before due and a regressed wall clock perform no operation.
	restarted := testScheduler(t, store, clock, client, nil)
	clock.set(now.Add(-time.Hour))
	if err := restarted.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.registerCalls != 1 || client.heartbeatCalls != 0 {
		t.Fatalf("premature calls = register:%d heartbeat:%d", client.registerCalls, client.heartbeatCalls)
	}

	clock.set(state.NextAttempt)
	if err := restarted.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.heartbeatCalls != 1 || store.states[testOrigin].LastOutcome != "heartbeat" {
		t.Fatalf("heartbeat calls=%d state=%#v", client.heartbeatCalls, store.states[testOrigin])
	}
}

func TestLeaseContentionAndDurableDisablePreventTraffic(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	store := newFakeStore()
	store.contend = true
	client := &fakeClient{}
	scheduler := testScheduler(t, store, &fakeClock{now: now}, client, nil)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.contend = false
	scheduler.enabled = func(string) (bool, error) { return false, nil }
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.registerCalls != 0 || client.heartbeatCalls != 0 {
		t.Fatalf("disabled/contended scheduler made calls: %#v", client)
	}
}

func TestDurableEnablementIsRecheckedAfterLeaseAcquisition(t *testing.T) {
	store := newFakeStore()
	client := &fakeClient{}
	checks := 0
	scheduler := testScheduler(
		t,
		store,
		&fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
		client,
		func(string) (bool, error) {
			checks++
			return checks == 1, nil
		},
	)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if checks != 2 || client.registerCalls != 0 || client.heartbeatCalls != 0 {
		t.Fatalf("checks=%d client=%#v", checks, client)
	}
}

func TestTwoSchedulersProduceAtMostOneOperationPerDueSlot(t *testing.T) {
	store := newFakeStore()
	clock := &fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	client := &fakeClient{}
	first := testScheduler(t, store, clock, client, nil)
	second := testScheduler(t, store, clock, client, nil)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); _ = first.RunOnce(context.Background()) }()
	go func() { defer wait.Done(); _ = second.RunOnce(context.Background()) }()
	wait.Wait()
	if client.registerCalls != 1 {
		t.Fatalf("register calls = %d", client.registerCalls)
	}
}

func TestLeaseLossCancelsOperationWithoutStaleStateWrite(t *testing.T) {
	store := newFakeStore()
	store.renew = false
	renew := make(chan time.Time, 1)
	renew <- time.Now()
	clock := &fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC), after: renew}
	client := &fakeClient{waitForCancel: true}
	scheduler := testScheduler(t, store, clock, client, nil)
	if err := scheduler.RunOnce(context.Background()); err == nil {
		t.Fatal("lease loss was not reported")
	}
	if store.stateCount() != 0 {
		t.Fatalf("former owner persisted state after lease loss: %#v", store.states)
	}
}

func TestRetryClassificationAndBoundedBackoff(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		outcome    string
		diagnostic string
		retrying   bool
	}{
		{"transport", directoryclient.ErrDirectoryTransport, "retrying", "transport", true},
		{"rate", &directoryclient.ProtocolError{Code: directoryclient.ErrorRateLimited, RetryAfter: 20 * time.Minute}, "retrying", "rate_limited", true},
		{"internal", &directoryclient.ProtocolError{Code: directoryclient.ErrorInternal}, "retrying", "internal", true},
		{"authentication", &directoryclient.ProtocolError{Code: directoryclient.ErrorAuthenticationFailed}, "authentication", "authentication", false},
		{"suspended", &directoryclient.ProtocolError{Code: directoryclient.ErrorRelaySuspended}, "suspended", "suspended", false},
		{"malformed", directoryclient.ErrDirectoryResponse, "malformed", "malformed", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
			store := newFakeStore()
			client := &fakeClient{err: test.err}
			scheduler := testScheduler(t, store, &fakeClock{now: now}, client, nil)
			if err := scheduler.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			state := store.states[testOrigin]
			if state.LastOutcome != test.outcome || state.Diagnostic != test.diagnostic {
				t.Fatalf("state = %#v", state)
			}
			if test.retrying {
				if state.NextAttempt.Before(now.Add(initialRetry)) || state.NextAttempt.After(now.Add(directoryclient.MaximumRetryAfter)) {
					t.Fatalf("retry time = %s", state.NextAttempt)
				}
			} else if state.NextAttempt.Before(now.Add(NominalHeartbeatInterval)) {
				t.Fatalf("closed failure retried early: %s", state.NextAttempt)
			}
		})
	}
}

func TestAcceleratedMultiDayStateRemainsBounded(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	client := &fakeClient{}
	scheduler := testScheduler(t, store, clock, client, nil)
	for day := 0; day < 45; day++ {
		clock.set(now.Add(time.Duration(day) * 27 * time.Hour))
		if err := scheduler.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.states) != 1 || store.states[testOrigin].Attempt != 0 {
		t.Fatalf("bounded state = %#v", store.states)
	}
}

func TestAcceleratedMultiDayClientUsesFreshSignedRequests(t *testing.T) {
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	keyPath, err := filepath.Abs(filepath.Join("..", "..", "misc", "test", "testKey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := models.LoadActorPrivateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	var signatures []string
	transport := schedulerRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		signatures = append(signatures, request.Header.Get("Signature-Input"))
		operation, status, outcome := "heartbeat", http.StatusOK, "recorded"
		if strings.HasSuffix(request.URL.Path, "/register") {
			operation, status, outcome = "register", http.StatusCreated, "created"
		}
		body := fmt.Sprintf(
			`{"protocol_version":1,"operation":%q,"outcome":%q,"relay_actor":%q}`,
			operation,
			outcome,
			testActor,
		)
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	nonce := 0
	client, err := directoryclient.New(directoryclient.Options{
		Origin:        testOrigin,
		RelayActor:    testActor,
		PublicBaseURL: "https://relay.example",
		KeyID:         testActor + "#main-key",
		PrivateKey:    key,
		HTTPClient:    &http.Client{Transport: transport},
		Now:           clock.Now,
		Nonce: func() (string, error) {
			nonce++
			return fmt.Sprintf("soak-nonce-%d", nonce), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduler := testScheduler(t, store, clock, client, nil)
	for operation := 0; operation < 45; operation++ {
		if err := scheduler.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock.set(store.states[testOrigin].NextAttempt)
	}
	if len(store.states) != 1 || len(signatures) != 45 || nonce != 45 {
		t.Fatalf("state=%d signatures=%d nonces=%d", len(store.states), len(signatures), nonce)
	}
	seen := make(map[string]struct{}, len(signatures))
	for index, signature := range signatures {
		if signature == "" {
			t.Fatal("signed request omitted Signature-Input")
		}
		wantNonce := fmt.Sprintf(`nonce="soak-nonce-%d"`, index+1)
		if !strings.Contains(signature, wantNonce) {
			t.Fatalf("signature %d omitted fresh nonce %q: %q", index, wantNonce, signature)
		}
		seen[signature] = struct{}{}
	}
	if len(seen) != len(signatures) {
		t.Fatalf("signature reuse detected: unique=%d total=%d", len(seen), len(signatures))
	}
}

func TestRunStopsCleanlyWhenContextIsCanceled(t *testing.T) {
	store := newFakeStore()
	clock := &fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)}
	scheduler := testScheduler(t, store, clock, &fakeClient{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()
	for store.stateCount() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
}

func TestShutdownCancellationDoesNotPersistAnOperationFailure(t *testing.T) {
	store := newFakeStore()
	client := &fakeClient{waitForCancel: true}
	scheduler := testScheduler(
		t,
		store,
		&fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
		client,
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = scheduler.RunOnce(ctx)
	}()
	for {
		registerCalls, _ := client.calls()
		if registerCalls == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("in-flight operation did not stop")
	}
	if store.stateCount() != 0 {
		t.Fatalf("shutdown persisted state: %#v", store.states)
	}
}

func TestRetryDelayNeverExceedsMaximumPlusJitter(t *testing.T) {
	for attempt := 1; attempt <= maximumAttempt; attempt++ {
		delay := retryDelay(attempt, testActor, testOrigin)
		if delay < initialRetry || delay > maximumLocalRetry {
			t.Fatalf("attempt %d delay = %s", attempt, delay)
		}
	}
	if stableJitter(testActor, testOrigin) < 0 || stableJitter(testActor, testOrigin) > MaximumStableJitter {
		t.Fatal("stable jitter escaped bounds")
	}
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	scheduler := testScheduler(t, newFakeStore(), &fakeClock{now: now}, &fakeClient{}, nil)
	state := State{}
	scheduler.applyFailure(&state, testOrigin, now, &directoryclient.ProtocolError{
		Code:       directoryclient.ErrorRateLimited,
		RetryAfter: 48 * time.Hour,
	})
	if state.NextAttempt != now.Add(directoryclient.MaximumRetryAfter) {
		t.Fatalf("Retry-After cap = %s, want %s", state.NextAttempt.Sub(now), directoryclient.MaximumRetryAfter)
	}
}
