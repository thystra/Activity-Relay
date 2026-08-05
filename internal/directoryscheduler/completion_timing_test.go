package directoryscheduler

import (
	"context"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
)

type completionAdvancingClient struct {
	clock    *fakeClock
	advance  time.Duration
	response directoryclient.Response
	err      error
}

func (client *completionAdvancingClient) complete() (directoryclient.Response, error) {
	client.clock.set(client.clock.Now().Add(client.advance))
	return client.response, client.err
}

func (client *completionAdvancingClient) Register(context.Context) (directoryclient.Response, error) {
	return client.complete()
}

func (client *completionAdvancingClient) HeartbeatWithRegisterReconciliation(context.Context) (directoryclient.Response, error) {
	return client.complete()
}

func TestRetryAfterIsMeasuredFromRequestCompletion(t *testing.T) {
	startedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: startedAt}
	store := newFakeStore()
	client := &completionAdvancingClient{
		clock:   clock,
		advance: 20 * time.Second,
		err: &directoryclient.ProtocolError{
			Code:       directoryclient.ErrorRateLimited,
			RetryAfter: 2 * time.Minute,
		},
	}
	scheduler := testScheduler(t, store, clock, client, nil)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	completedAt := startedAt.Add(client.advance)
	state := store.states[testOrigin]
	if state.LastObserved != completedAt {
		t.Fatalf("LastObserved = %s, want completion %s", state.LastObserved, completedAt)
	}
	if state.NextAttempt != completedAt.Add(2*time.Minute) {
		t.Fatalf("NextAttempt = %s, want %s", state.NextAttempt, completedAt.Add(2*time.Minute))
	}
}

func TestSuccessHeartbeatDeadlineIsMeasuredFromRequestCompletion(t *testing.T) {
	startedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: startedAt}
	store := newFakeStore()
	client := &completionAdvancingClient{
		clock:   clock,
		advance: 45 * time.Second,
		response: directoryclient.Response{
			Operation: directoryclient.OperationRegister,
			Outcome:   directoryclient.OutcomeCreated,
		},
	}
	scheduler := testScheduler(t, store, clock, client, nil)
	if err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	completedAt := startedAt.Add(client.advance)
	state := store.states[testOrigin]
	wantNext := completedAt.Add(NominalHeartbeatInterval + stableJitter(testActor, testOrigin))
	if state.LastSuccess != completedAt || state.LastObserved != completedAt {
		t.Fatalf("completion timestamps = last_success:%s last_observed:%s want:%s", state.LastSuccess, state.LastObserved, completedAt)
	}
	if state.NextAttempt != wantNext {
		t.Fatalf("NextAttempt = %s, want %s", state.NextAttempt, wantNext)
	}
}
