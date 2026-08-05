package directoryscheduler

import (
	"context"
	"testing"
	"time"
)

func TestRedisFormerOwnerCannotOverwriteNewOwnerState(t *testing.T) {
	store, client := redisSchedulerStore(t)
	ctx := context.Background()
	origin := "https://directory-store-fencing.example"
	stateKey := directoryKey(origin, "state")
	leaseKey := directoryKey(origin, "lease")
	t.Cleanup(func() { _ = client.Del(ctx, stateKey, leaseKey).Err() })

	first, acquired, err := store.Acquire(ctx, origin, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first Acquire() = (%v, %t, %v)", first, acquired, err)
	}
	firstState := State{Registered: true, LastOutcome: "registered", Diagnostic: "none"}
	if owned, err := store.SaveOwned(ctx, origin, first, firstState); err != nil || !owned {
		t.Fatalf("first SaveOwned() = (%t, %v)", owned, err)
	}

	// Simulate expiry before a second process acquires the lease.
	if err := client.Del(ctx, leaseKey).Err(); err != nil {
		t.Fatal(err)
	}
	second, acquired, err := store.Acquire(ctx, origin, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("second Acquire() = (%v, %t, %v)", second, acquired, err)
	}
	secondState := State{Registered: true, LastOutcome: "heartbeat", Diagnostic: "none"}
	if owned, err := store.SaveOwned(ctx, origin, second, secondState); err != nil || !owned {
		t.Fatalf("second SaveOwned() = (%t, %v)", owned, err)
	}

	staleState := State{Registered: false, LastOutcome: "retrying", Diagnostic: "lease_lost", Attempt: 1}
	if owned, err := store.SaveOwned(ctx, origin, first, staleState); err != nil || owned {
		t.Fatalf("stale SaveOwned() = (%t, %v)", owned, err)
	}
	got, err := store.Load(ctx, origin)
	if err != nil || got != secondState {
		t.Fatalf("Load() = (%#v, %v), want %#v", got, err, secondState)
	}
}
