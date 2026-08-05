package directoryscheduler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix      = "relay:directory:v1:"
	stateRetention = 90 * 24 * time.Hour
	maximumAttempt = 16
)

var ErrStore = errors.New("directory scheduler store failed")

type State struct {
	Registered   bool
	LastSuccess  time.Time
	NextAttempt  time.Time
	LastOutcome  string
	Diagnostic   string
	Attempt      int
	LastObserved time.Time
}

type Lease interface {
	Renew(context.Context, time.Duration) (bool, error)
	Release(context.Context) error
}

type StateStore interface {
	Load(context.Context, string) (State, error)
	Save(context.Context, string, State) error
	SaveOwned(context.Context, string, Lease, State) (bool, error)
	Acquire(context.Context, string, time.Duration) (Lease, bool, error)
}

type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) (*RedisStore, error) {
	if client == nil {
		return nil, ErrStore
	}
	return &RedisStore{client: client}, nil
}

func directoryKey(origin, suffix string) string {
	digest := sha256.Sum256([]byte(origin))
	return keyPrefix + hex.EncodeToString(digest[:]) + ":" + suffix
}

func (store *RedisStore) Load(ctx context.Context, origin string) (State, error) {
	if store == nil || store.client == nil || ctx == nil || origin == "" {
		return State{}, ErrStore
	}
	values, err := store.client.HGetAll(ctx, directoryKey(origin, "state")).Result()
	if err != nil {
		return State{}, ErrStore
	}
	if len(values) == 0 {
		return State{}, nil
	}
	if values["registered"] != "0" && values["registered"] != "1" {
		return State{}, ErrStore
	}
	state := State{
		Registered:  values["registered"] == "1",
		LastOutcome: values["last_outcome"],
		Diagnostic:  values["diagnostic"],
	}
	state.LastSuccess, err = parseTime(values["last_success_unix"])
	if err != nil {
		return State{}, ErrStore
	}
	state.NextAttempt, err = parseTime(values["next_attempt_unix"])
	if err != nil {
		return State{}, ErrStore
	}
	state.LastObserved, err = parseTime(values["last_observed_unix"])
	if err != nil {
		return State{}, ErrStore
	}
	if values["attempt"] != "" {
		attempt, parseErr := strconv.ParseUint(values["attempt"], 10, 8)
		if parseErr != nil || attempt > maximumAttempt {
			return State{}, ErrStore
		}
		state.Attempt = int(attempt)
	}
	if !validOutcome(state.LastOutcome) || !validDiagnostic(state.Diagnostic) {
		return State{}, ErrStore
	}
	return state, nil
}

func validateState(origin string, state State) error {
	if origin == "" || state.Attempt < 0 || state.Attempt > maximumAttempt ||
		!validOutcome(state.LastOutcome) || !validDiagnostic(state.Diagnostic) {
		return ErrStore
	}
	return nil
}

func stateArguments(state State) []any {
	registered := "0"
	if state.Registered {
		registered = "1"
	}
	return []any{
		registered,
		unixString(state.LastSuccess),
		unixString(state.NextAttempt),
		state.LastOutcome,
		state.Diagnostic,
		strconv.Itoa(state.Attempt),
		unixString(state.LastObserved),
	}
}

func (store *RedisStore) Save(ctx context.Context, origin string, state State) error {
	if store == nil || store.client == nil || ctx == nil || validateState(origin, state) != nil {
		return ErrStore
	}
	key := directoryKey(origin, "state")
	arguments := stateArguments(state)
	pipeline := store.client.TxPipeline()
	pipeline.HSet(ctx, key, map[string]any{
		"registered":         arguments[0],
		"last_success_unix":  arguments[1],
		"next_attempt_unix":  arguments[2],
		"last_outcome":       arguments[3],
		"diagnostic":         arguments[4],
		"attempt":            arguments[5],
		"last_observed_unix": arguments[6],
	})
	pipeline.Expire(ctx, key, stateRetention)
	if _, err := pipeline.Exec(ctx); err != nil {
		return ErrStore
	}
	return nil
}

var saveOwnedScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then
  return 0
end
redis.call('HSET', KEYS[2],
  'registered', ARGV[2],
  'last_success_unix', ARGV[3],
  'next_attempt_unix', ARGV[4],
  'last_outcome', ARGV[5],
  'diagnostic', ARGV[6],
  'attempt', ARGV[7],
  'last_observed_unix', ARGV[8])
redis.call('PEXPIRE', KEYS[2], ARGV[9])
return 1
`)

func (store *RedisStore) SaveOwned(
	ctx context.Context,
	origin string,
	lease Lease,
	state State,
) (bool, error) {
	ownedLease, ok := lease.(*redisLease)
	if store == nil || store.client == nil || ctx == nil || !ok || ownedLease == nil ||
		ownedLease.client != store.client || ownedLease.key != directoryKey(origin, "lease") ||
		validateState(origin, state) != nil {
		return false, ErrStore
	}
	arguments := append([]any{ownedLease.value}, stateArguments(state)...)
	arguments = append(arguments, stateRetention.Milliseconds())
	result, err := saveOwnedScript.Run(
		ctx,
		store.client,
		[]string{ownedLease.key, directoryKey(origin, "state")},
		arguments...,
	).Int64()
	if err != nil {
		return false, ErrStore
	}
	return result == 1, nil
}

func (store *RedisStore) Acquire(
	ctx context.Context,
	origin string,
	ttl time.Duration,
) (Lease, bool, error) {
	if store == nil || store.client == nil || ctx == nil || origin == "" || ttl <= 0 {
		return nil, false, ErrStore
	}
	value, err := leaseToken()
	if err != nil {
		return nil, false, ErrStore
	}
	key := directoryKey(origin, "lease")
	acquired, err := store.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return nil, false, ErrStore
	}
	if !acquired {
		return nil, false, nil
	}
	return &redisLease{client: store.client, key: key, value: value}, true, nil
}

type redisLease struct {
	client *redis.Client
	key    string
	value  string
}

var renewLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`)

var releaseLeaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

func (lease *redisLease) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	if lease == nil || lease.client == nil || ctx == nil || ttl <= 0 {
		return false, ErrStore
	}
	result, err := renewLeaseScript.Run(
		ctx, lease.client, []string{lease.key}, lease.value, ttl.Milliseconds(),
	).Int64()
	if err != nil {
		return false, ErrStore
	}
	return result == 1, nil
}

func (lease *redisLease) Release(ctx context.Context) error {
	if lease == nil || lease.client == nil || ctx == nil {
		return ErrStore
	}
	if _, err := releaseLeaseScript.Run(
		ctx, lease.client, []string{lease.key}, lease.value,
	).Result(); err != nil {
		return ErrStore
	}
	return nil
}

func leaseToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func parseTime(value string) (time.Time, error) {
	if value == "" || value == "0" {
		return time.Time{}, nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 {
		return time.Time{}, ErrStore
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func unixString(value time.Time) string {
	if value.IsZero() {
		return "0"
	}
	return strconv.FormatInt(value.UTC().Unix(), 10)
}

func validOutcome(value string) bool {
	switch value {
	case "", "registered", "heartbeat", "retrying", "authentication", "policy", "suspended", "malformed", "disabled":
		return true
	default:
		return false
	}
}

func validDiagnostic(value string) bool {
	switch value {
	case "", "none", "transport", "internal", "rate_limited", "authentication", "enrollment", "suspended", "lifecycle", "malformed", "lease_lost", "disabled":
		return true
	default:
		return false
	}
}
