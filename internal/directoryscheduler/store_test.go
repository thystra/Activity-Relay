package directoryscheduler

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func redisSchedulerStore(t *testing.T) (*RedisStore, *redis.Client) {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL is required")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	store, err := NewRedisStore(client)
	if err != nil {
		t.Fatal(err)
	}
	return store, client
}

func TestRedisStorePersistsBoundedStateAndOwnsLeaseToken(t *testing.T) {
	store, client := redisSchedulerStore(t)
	ctx := context.Background()
	origin := "https://directory-store-test.example"
	t.Cleanup(func() {
		_ = client.Del(ctx, directoryKey(origin, "state"), directoryKey(origin, "lease")).Err()
	})
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	want := State{
		Registered: true, LastSuccess: now, NextAttempt: now.Add(time.Hour),
		LastOutcome: "heartbeat", Diagnostic: "none", Attempt: 0, LastObserved: now,
	}
	if err := store.Save(ctx, origin, want); err != nil {
		t.Fatal(err)
	}
	if ttl := client.TTL(ctx, directoryKey(origin, "state")).Val(); ttl <= 0 || ttl > stateRetention {
		t.Fatalf("state TTL = %s", ttl)
	}
	keys, err := client.Keys(ctx, keyPrefix+"*").Result()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if strings.Contains(key, "directory-store-test") || strings.Contains(key, origin) {
			t.Fatalf("Redis key exposed raw origin: %q", key)
		}
	}
	got, err := store.Load(ctx, origin)
	if err != nil || got != want {
		t.Fatalf("Load() = (%#v, %v), want %#v", got, err, want)
	}
	lease, acquired, err := store.Acquire(ctx, origin, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("Acquire() = (%v, %t, %v)", lease, acquired, err)
	}
	if _, acquired, err := store.Acquire(ctx, origin, time.Minute); err != nil || acquired {
		t.Fatalf("contended Acquire() = (%t, %v)", acquired, err)
	}
	if renewed, err := lease.Renew(ctx, time.Minute); err != nil || !renewed {
		t.Fatalf("Renew() = (%t, %v)", renewed, err)
	}
	if err := lease.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := store.Acquire(ctx, origin, time.Minute); err != nil || !acquired {
		t.Fatalf("Acquire() after release = (%t, %v)", acquired, err)
	}
}

func TestRedisStoreRejectsUnknownPersistedVocabulary(t *testing.T) {
	store, client := redisSchedulerStore(t)
	ctx := context.Background()
	origin := "https://directory-store-invalid.example"
	key := directoryKey(origin, "state")
	t.Cleanup(func() { _ = client.Del(ctx, key).Err() })
	if err := client.HSet(ctx, key, "last_outcome", "private-unbounded-value").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, origin); err == nil {
		t.Fatal("unknown persisted vocabulary was accepted")
	}
}

func TestRedisStoreRejectsInvalidRegisteredEncoding(t *testing.T) {
	store, client := redisSchedulerStore(t)
	ctx := context.Background()
	origin := "https://directory-store-invalid-registration.example"
	key := directoryKey(origin, "state")
	t.Cleanup(func() { _ = client.Del(ctx, key).Err() })
	if err := client.HSet(ctx, key, map[string]any{
		"registered":   "maybe",
		"last_outcome": "registered",
		"diagnostic":   "none",
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, origin); err == nil {
		t.Fatal("invalid registered encoding was accepted")
	}
}
