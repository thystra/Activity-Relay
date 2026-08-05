package directoryscheduler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
)

type schedulerTimer struct {
	delay time.Duration
	fire  chan time.Time
}

type deadlineClock struct {
	mu     sync.Mutex
	now    time.Time
	timers chan schedulerTimer
}

func newDeadlineClock(now time.Time) *deadlineClock {
	return &deadlineClock{
		now:    now,
		timers: make(chan schedulerTimer, 16),
	}
}

func (clock *deadlineClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *deadlineClock) After(delay time.Duration) <-chan time.Time {
	fire := make(chan time.Time, 1)
	clock.timers <- schedulerTimer{delay: delay, fire: fire}
	return fire
}

func (clock *deadlineClock) advanceAndFire(timer schedulerTimer) {
	clock.mu.Lock()
	clock.now = clock.now.Add(timer.delay)
	now := clock.now
	clock.mu.Unlock()
	timer.fire <- now
}

type retryThenSuccessClient struct {
	calls      atomic.Int32
	secondCall chan struct{}
}

func (client *retryThenSuccessClient) Register(context.Context) (directoryclient.Response, error) {
	call := client.calls.Add(1)
	if call == 1 {
		return directoryclient.Response{}, directoryclient.ErrDirectoryTransport
	}
	if call == 2 {
		close(client.secondCall)
	}
	return directoryclient.Response{
		Operation: directoryclient.OperationRegister,
		Outcome:   directoryclient.OutcomeCreated,
	}, nil
}

func (client *retryThenSuccessClient) HeartbeatWithRegisterReconciliation(context.Context) (directoryclient.Response, error) {
	return directoryclient.Response{
		Operation: directoryclient.OperationHeartbeat,
		Outcome:   directoryclient.OutcomeRecorded,
	}, nil
}

func nextRunLoopTimer(t *testing.T, clock *deadlineClock, want time.Duration) schedulerTimer {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case timer := <-clock.timers:
			// Each immediate lifecycle operation also creates a lease-renewal timer.
			// It is abandoned when the operation context is canceled.
			if timer.delay == leaseRenewInterval {
				continue
			}
			if timer.delay != want {
				t.Fatalf("scheduler sleep = %s, want %s", timer.delay, want)
			}
			return timer
		case <-deadline:
			t.Fatalf("scheduler did not request %s wake", want)
		}
	}
}

func TestRunWakesAtPersistedRetryDeadlineAndRetainsMinuteObservationPoll(t *testing.T) {
	startedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock := newDeadlineClock(startedAt)
	store := newFakeStore()
	client := &retryThenSuccessClient{secondCall: make(chan struct{})}
	scheduler := testScheduler(t, store, clock, client, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("scheduler did not stop")
		}
	})

	wantRetry := retryDelay(1, testActor, testOrigin)
	retryTimer := nextRunLoopTimer(t, clock, wantRetry)
	if calls := client.calls.Load(); calls != 1 {
		t.Fatalf("calls before retry deadline = %d, want 1", calls)
	}

	store.mu.Lock()
	state := store.states[testOrigin]
	store.mu.Unlock()
	if state.NextAttempt != startedAt.Add(wantRetry) {
		t.Fatalf("persisted NextAttempt = %s, want %s", state.NextAttempt, startedAt.Add(wantRetry))
	}

	clock.advanceAndFire(retryTimer)
	select {
	case <-client.secondCall:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not retry at the persisted deadline")
	}

	minuteTimer := nextRunLoopTimer(t, clock, SchedulerPollInterval)
	if minuteTimer.delay != SchedulerPollInterval {
		t.Fatalf("successful daily schedule sleep = %s, want observation poll %s", minuteTimer.delay, SchedulerPollInterval)
	}
}
