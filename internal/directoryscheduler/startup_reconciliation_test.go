package directoryscheduler

import (
	"context"
	"testing"
	"time"
)

func TestProcessStartupReconcilesBeforePersistedHeartbeatDeadline(t *testing.T) {
	now := time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC)
	store := newFakeStore()
	store.states[testOrigin] = State{
		Registered:   true,
		LastSuccess:  now.Add(-4 * time.Hour),
		NextAttempt:  now.Add(20 * time.Hour),
		LastOutcome:  "heartbeat",
		Diagnostic:   "none",
		LastObserved: now.Add(-4 * time.Hour),
	}
	client := &fakeClient{}
	scheduler := testScheduler(t, store, &fakeClock{now: now}, client, nil)

	next, err := scheduler.runCycle(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	state := store.states[testOrigin]
	wantNext := NextHeartbeatAttempt(testActor, testOrigin, now)
	registerCalls, heartbeatCalls := client.calls()
	if registerCalls != 0 || heartbeatCalls != 1 || state.LastOutcome != "heartbeat" ||
		state.LastSuccess != now || state.NextAttempt != wantNext || next != wantNext {
		t.Fatalf("calls=(%d,%d) state=%#v next=%s want=%s", registerCalls, heartbeatCalls, state, next, wantNext)
	}

	// Keeping the process startup boundary for later cycles must not cause a
	// second pulse after this process has recorded its startup reconciliation.
	if _, err := scheduler.runCycle(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	registerCalls, heartbeatCalls = client.calls()
	if registerCalls != 0 || heartbeatCalls != 1 {
		t.Fatalf("startup reconciliation repeated: register=%d heartbeat=%d", registerCalls, heartbeatCalls)
	}
}

func TestProcessStartupDoesNotBypassPersistedFailureDeadline(t *testing.T) {
	now := time.Date(2026, 10, 6, 4, 0, 0, 0, time.UTC)
	deadline := now.Add(2 * time.Hour)
	store := newFakeStore()
	store.states[testOrigin] = State{
		Registered:   true,
		LastSuccess:  now.Add(-24 * time.Hour),
		NextAttempt:  deadline,
		LastOutcome:  "retrying",
		Diagnostic:   "rate_limited",
		Attempt:      1,
		LastObserved: now.Add(-time.Hour),
	}
	client := &fakeClient{}
	scheduler := testScheduler(t, store, &fakeClock{now: now}, client, nil)

	next, err := scheduler.runCycle(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	registerCalls, heartbeatCalls := client.calls()
	if registerCalls != 0 || heartbeatCalls != 0 || next != deadline {
		t.Fatalf("startup bypassed failure deadline: calls=(%d,%d) next=%s want=%s", registerCalls, heartbeatCalls, next, deadline)
	}
}
