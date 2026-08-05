package directoryscheduler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
)

const (
	NominalHeartbeatInterval = 24 * time.Hour
	MaximumStableJitter      = 2 * time.Hour
	SchedulerPollInterval    = time.Minute
	minimumSchedulerSleep    = time.Second
	leaseTTL                 = time.Minute
	leaseRenewInterval       = 20 * time.Second
	initialRetry             = 30 * time.Second
	maximumLocalRetry        = 15 * time.Minute
)

type Client interface {
	Register(context.Context) (directoryclient.Response, error)
	HeartbeatWithRegisterReconciliation(context.Context) (directoryclient.Response, error)
}

type ClientFactory func(directoryclient.Directory) (Client, error)
type Enabled func(string) (bool, error)

type Metrics interface {
	RecordDirectory(context.Context, string, string) error
}

type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                             { return time.Now() }
func (realClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }

type Config struct {
	RelayActor  string
	Directories []directoryclient.Directory
	Store       StateStore
	Enabled     Enabled
	Clients     ClientFactory
	Metrics     Metrics
	Clock       Clock
}

type Scheduler struct {
	relayActor  string
	directories []directoryclient.Directory
	store       StateStore
	enabled     Enabled
	clients     ClientFactory
	metrics     Metrics
	clock       Clock
	mu          sync.Mutex
	lastNow     time.Time
}

func New(config Config) (*Scheduler, error) {
	if config.RelayActor == "" || config.Store == nil || config.Enabled == nil ||
		config.Clients == nil || len(config.Directories) == 0 ||
		len(config.Directories) > directoryclient.MaximumDirectories {
		return nil, errors.New("directory scheduler configuration is invalid")
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &Scheduler{
		relayActor:  config.RelayActor,
		directories: append([]directoryclient.Directory(nil), config.Directories...),
		store:       config.Store,
		enabled:     config.Enabled,
		clients:     config.Clients,
		metrics:     config.Metrics,
		clock:       config.Clock,
	}, nil
}

func (scheduler *Scheduler) Run(ctx context.Context) {
	if scheduler == nil || ctx == nil {
		return
	}
	for {
		nextAttempt, _ := scheduler.runCycle(ctx)
		if ctx.Err() != nil {
			return
		}
		delay := scheduler.nextWakeDelay(nextAttempt)
		select {
		case <-ctx.Done():
			return
		case <-scheduler.clock.After(delay):
		}
	}
}

func (scheduler *Scheduler) RunOnce(ctx context.Context) error {
	_, err := scheduler.runCycle(ctx)
	return err
}

func (scheduler *Scheduler) runCycle(ctx context.Context) (time.Time, error) {
	if scheduler == nil || ctx == nil {
		return time.Time{}, errors.New("directory scheduler is unavailable")
	}
	var earliest time.Time
	var first error
	for _, entry := range scheduler.directories {
		if !entry.Enabled {
			continue
		}
		nextAttempt, err := scheduler.runDirectory(ctx, entry)
		if !nextAttempt.IsZero() && (earliest.IsZero() || nextAttempt.Before(earliest)) {
			earliest = nextAttempt
		}
		if err != nil && first == nil {
			first = err
		}
	}
	return earliest, first
}

func (scheduler *Scheduler) nextWakeDelay(nextAttempt time.Time) time.Duration {
	delay := SchedulerPollInterval
	if !nextAttempt.IsZero() {
		until := nextAttempt.Sub(scheduler.monotonicNow(time.Time{}))
		if until < delay {
			delay = until
		}
	}
	if delay < minimumSchedulerSleep {
		return minimumSchedulerSleep
	}
	return delay
}

func (scheduler *Scheduler) runDirectory(ctx context.Context, entry directoryclient.Directory) (time.Time, error) {
	enabled, err := scheduler.enabled(entry.Origin)
	if err != nil || !enabled {
		if err != nil {
			scheduler.record(ctx, "failure", "internal")
		}
		return time.Time{}, err
	}
	state, err := scheduler.store.Load(ctx, entry.Origin)
	if err != nil {
		scheduler.record(ctx, "failure", "internal")
		return time.Time{}, err
	}
	now := scheduler.monotonicNow(state.LastObserved)
	if !state.NextAttempt.IsZero() && now.Before(state.NextAttempt) {
		return state.NextAttempt, nil
	}
	lease, acquired, err := scheduler.store.Acquire(ctx, entry.Origin, leaseTTL)
	if err != nil || !acquired {
		if err != nil {
			scheduler.record(ctx, "failure", "internal")
		}
		return time.Time{}, err
	}
	defer releaseLease(lease)

	// Recheck durable configuration only after owning the same lease used by
	// unregister. This closes the disable/register race.
	state, err = scheduler.store.Load(ctx, entry.Origin)
	if err != nil {
		scheduler.record(ctx, "failure", "internal")
		return time.Time{}, err
	}
	now = scheduler.monotonicNow(state.LastObserved)
	if !state.NextAttempt.IsZero() && now.Before(state.NextAttempt) {
		return state.NextAttempt, nil
	}
	enabled, err = scheduler.enabled(entry.Origin)
	if err != nil || !enabled {
		if err != nil {
			scheduler.record(ctx, "failure", "internal")
		}
		return time.Time{}, err
	}
	client, err := scheduler.clients(entry)
	if err != nil {
		scheduler.applyFailure(&state, entry.Origin, now, directoryclient.ErrDirectoryResponse)
		if saveErr := scheduler.saveOwned(ctx, entry.Origin, lease, state); saveErr != nil {
			return time.Time{}, saveErr
		}
		scheduler.record(ctx, "failure", state.Diagnostic)
		return state.NextAttempt, nil
	}
	operationContext, cancel := context.WithCancel(ctx)
	defer cancel()
	lost := make(chan struct{}, 1)
	renewalDone := make(chan struct{})
	go func() {
		defer close(renewalDone)
		renewLease(operationContext, scheduler.clock, lease, cancel, lost)
	}()

	var response directoryclient.Response
	if state.Registered {
		response, err = client.HeartbeatWithRegisterReconciliation(operationContext)
	} else {
		response, err = client.Register(operationContext)
	}
	cancel()
	<-renewalDone
	select {
	case <-lost:
		// Ownership is already gone. Recording a metric is safe, but writing
		// state here could overwrite the new owner's reconciliation result.
		scheduler.record(ctx, "failure", "lease_lost")
		return time.Time{}, ErrStore
	default:
	}
	if ctx.Err() != nil {
		return time.Time{}, ctx.Err()
	}
	completedAt := scheduler.monotonicNow(state.LastObserved)
	if err == nil {
		state.Registered = true
		state.LastSuccess = completedAt
		state.NextAttempt = completedAt.Add(NominalHeartbeatInterval + stableJitter(scheduler.relayActor, entry.Origin))
		state.Attempt = 0
		state.Diagnostic = "none"
		if response.Operation == directoryclient.OperationRegister {
			state.LastOutcome = "registered"
		} else {
			state.LastOutcome = "heartbeat"
		}
		state.LastObserved = completedAt
		if saveErr := scheduler.saveOwned(ctx, entry.Origin, lease, state); saveErr != nil {
			return time.Time{}, saveErr
		}
		scheduler.record(ctx, "success", "none")
		return state.NextAttempt, nil
	}
	scheduler.applyFailure(&state, entry.Origin, completedAt, err)
	if saveErr := scheduler.saveOwned(ctx, entry.Origin, lease, state); saveErr != nil {
		return time.Time{}, saveErr
	}
	scheduler.record(ctx, "failure", state.Diagnostic)
	return state.NextAttempt, nil
}

func (scheduler *Scheduler) monotonicNow(persisted time.Time) time.Time {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	now := scheduler.clock.Now().UTC().Truncate(time.Second)
	if now.Before(scheduler.lastNow) {
		now = scheduler.lastNow
	}
	if now.Before(persisted) {
		now = persisted
	}
	scheduler.lastNow = now
	return now
}

func (scheduler *Scheduler) applyFailure(state *State, origin string, now time.Time, err error) {
	state.LastObserved = now
	var protocolError *directoryclient.ProtocolError
	switch {
	case errors.Is(err, directoryclient.ErrDirectoryTransport):
		state.LastOutcome, state.Diagnostic = "retrying", "transport"
	case errors.As(err, &protocolError) && protocolError.Code == directoryclient.ErrorRateLimited:
		state.LastOutcome, state.Diagnostic = "retrying", "rate_limited"
	case errors.As(err, &protocolError) && protocolError.Code == directoryclient.ErrorInternal:
		state.LastOutcome, state.Diagnostic = "retrying", "internal"
	case errors.As(err, &protocolError) && protocolError.Code == directoryclient.ErrorAuthenticationFailed:
		state.LastOutcome, state.Diagnostic = "authentication", "authentication"
	case errors.As(err, &protocolError) && protocolError.Code == directoryclient.ErrorRelaySuspended:
		state.LastOutcome, state.Diagnostic = "suspended", "suspended"
	case errors.As(err, &protocolError) && protocolError.Code == directoryclient.ErrorEnrollmentClosed:
		state.LastOutcome, state.Diagnostic = "policy", "enrollment"
	case errors.As(err, &protocolError) && protocolError.Code == directoryclient.ErrorLifecycleUnavailable:
		state.LastOutcome, state.Diagnostic = "policy", "lifecycle"
	default:
		state.LastOutcome, state.Diagnostic = "malformed", "malformed"
	}
	if state.LastOutcome != "retrying" {
		state.Attempt = 0
		state.NextAttempt = now.Add(NominalHeartbeatInterval + stableJitter(scheduler.relayActor, origin))
		return
	}
	state.Attempt++
	if state.Attempt > maximumAttempt {
		state.Attempt = maximumAttempt
	}
	localDelay := retryDelay(state.Attempt, scheduler.relayActor, origin)
	delay := localDelay
	if protocolError != nil && protocolError.RetryAfter > delay {
		delay = protocolError.RetryAfter
	}
	if delay > directoryclient.MaximumRetryAfter {
		delay = directoryclient.MaximumRetryAfter
	}
	state.NextAttempt = now.Add(delay)
}

func (scheduler *Scheduler) saveOwned(
	ctx context.Context,
	origin string,
	lease Lease,
	state State,
) error {
	owned, err := scheduler.store.SaveOwned(ctx, origin, lease, state)
	if err != nil {
		scheduler.record(ctx, "failure", "internal")
		return err
	}
	if !owned {
		scheduler.record(ctx, "failure", "lease_lost")
		return ErrStore
	}
	return nil
}

func stableJitter(relayActor, origin string) time.Duration {
	digest := sha256.Sum256([]byte(relayActor + "\x00" + origin + "\x00daily"))
	seconds := uint64(MaximumStableJitter/time.Second) + 1
	return time.Duration(binary.BigEndian.Uint64(digest[:8])%seconds) * time.Second
}

func retryDelay(attempt int, relayActor, origin string) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := initialRetry << (attempt - 1)
	if base > maximumLocalRetry {
		base = maximumLocalRetry
	}
	digest := sha256.Sum256([]byte(relayActor + "\x00" + origin + "\x00retry\x00" + strconv.Itoa(attempt)))
	jitterRange := base / 5
	if jitterRange <= 0 {
		return base
	}
	jitterSeconds := uint64(jitterRange / time.Second)
	delay := base + time.Duration(binary.BigEndian.Uint64(digest[:8])%(jitterSeconds+1))*time.Second
	if delay > maximumLocalRetry {
		return maximumLocalRetry
	}
	return delay
}

func releaseLease(lease Lease) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = lease.Release(ctx)
}

func renewLease(ctx context.Context, clock Clock, lease Lease, cancel context.CancelFunc, lost chan<- struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-clock.After(leaseRenewInterval):
			ok, err := lease.Renew(ctx, leaseTTL)
			if ctx.Err() != nil {
				return
			}
			if err != nil || !ok {
				select {
				case lost <- struct{}{}:
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (scheduler *Scheduler) record(ctx context.Context, result, diagnostic string) {
	if scheduler.metrics != nil {
		_ = scheduler.metrics.RecordDirectory(ctx, result, diagnostic)
	}
}
