package directoryscheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
)

func (store *fakeStore) SaveOwned(
	ctx context.Context,
	origin string,
	lease Lease,
	state State,
) (bool, error) {
	owned, ok := lease.(*releasingFakeLease)
	if !ok || owned.store != store || !store.renew {
		return false, nil
	}
	if err := store.Save(ctx, origin, state); err != nil {
		return false, err
	}
	return true, nil
}

func TestRemoteRetryAfterMateriallyLengthensShortLocalDelay(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	scheduler := testScheduler(t, newFakeStore(), &fakeClock{now: now}, &fakeClient{}, nil)

	state := State{}
	scheduler.applyFailure(&state, testOrigin, now, &directoryclient.ProtocolError{
		Code:       directoryclient.ErrorRateLimited,
		RetryAfter: 2 * time.Hour,
	})
	if state.NextAttempt != now.Add(2*time.Hour) {
		t.Fatalf("effective retry = %s, want 2h", state.NextAttempt.Sub(now))
	}

	state = State{}
	scheduler.applyFailure(&state, testOrigin, now, &directoryclient.ProtocolError{
		Code:       directoryclient.ErrorRateLimited,
		RetryAfter: 5 * time.Second,
	})
	wantLocal := retryDelay(1, testActor, testOrigin)
	if state.NextAttempt != now.Add(wantLocal) {
		t.Fatalf("short Retry-After reduced local delay: got %s want %s", state.NextAttempt.Sub(now), wantLocal)
	}
}

func TestRetryAfterCapAndLocalBackoffRemainAligned(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	scheduler := testScheduler(t, newFakeStore(), &fakeClock{now: now}, &fakeClient{}, nil)
	state := State{}
	scheduler.applyFailure(&state, testOrigin, now, &directoryclient.ProtocolError{
		Code:       directoryclient.ErrorRateLimited,
		RetryAfter: 7 * 24 * time.Hour,
	})
	if state.NextAttempt != now.Add(directoryclient.MaximumRetryAfter) {
		t.Fatalf("effective retry = %s, want cap %s", state.NextAttempt.Sub(now), directoryclient.MaximumRetryAfter)
	}
	for attempt := 1; attempt <= maximumAttempt; attempt++ {
		if delay := retryDelay(attempt, testActor, testOrigin); delay < initialRetry || delay > maximumLocalRetry {
			t.Fatalf("attempt %d local delay = %s", attempt, delay)
		}
	}
}

type errorEnabledMetrics struct {
	calls int
}

func (metrics *errorEnabledMetrics) RecordDirectory(context.Context, string, string) error {
	metrics.calls++
	return nil
}

func TestDurableDisableAndRemovalProduceNoTrafficOrFailureMetric(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled Enabled
	}{
		{name: "gate disabled", enabled: func(string) (bool, error) { return false, nil }},
		{name: "entry removed", enabled: func(string) (bool, error) { return false, nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeStore()
			client := &fakeClient{}
			metrics := &errorEnabledMetrics{}
			scheduler := testScheduler(
				t,
				store,
				&fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
				client,
				test.enabled,
			)
			scheduler.metrics = metrics
			if err := scheduler.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			register, heartbeat := client.calls()
			if register != 0 || heartbeat != 0 || metrics.calls != 0 || store.stateCount() != 0 {
				t.Fatalf("calls=(%d,%d) metrics=%d state=%#v", register, heartbeat, metrics.calls, store.states)
			}
		})
	}
}

type rejectedOwnershipStore struct {
	*fakeStore
}

func (store *rejectedOwnershipStore) SaveOwned(context.Context, string, Lease, State) (bool, error) {
	return false, nil
}

func TestOwnershipRejectionIsReportedAndDoesNotPersist(t *testing.T) {
	base := newFakeStore()
	store := &rejectedOwnershipStore{fakeStore: base}
	scheduler := testScheduler(
		t,
		store,
		&fakeClock{now: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)},
		&fakeClient{},
		nil,
	)
	if err := scheduler.RunOnce(context.Background()); !errors.Is(err, ErrStore) {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if base.stateCount() != 0 {
		t.Fatalf("ownership rejection persisted state: %#v", base.states)
	}
}

func TestRestartPreservesRemoteLengthenedRetrySchedule(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	client := &fakeClient{err: &directoryclient.ProtocolError{
		Code:       directoryclient.ErrorRateLimited,
		RetryAfter: 2 * time.Hour,
	}}
	first := testScheduler(t, store, clock, client, nil)
	if err := first.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := store.states[testOrigin].NextAttempt; got != now.Add(2*time.Hour) {
		t.Fatalf("persisted retry = %s", got.Sub(now))
	}
	registerCalls, _ := client.calls()
	if registerCalls != 1 {
		t.Fatalf("initial register calls = %d", registerCalls)
	}

	restarted := testScheduler(t, store, clock, client, nil)
	clock.set(now.Add(time.Hour))
	if err := restarted.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	registerCalls, _ = client.calls()
	if registerCalls != 1 {
		t.Fatalf("restart bypassed remote delay: calls=%d", registerCalls)
	}
}
