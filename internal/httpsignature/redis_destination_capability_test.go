package httpsignature

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisDestinationCapabilityStoreLifecycle(t *testing.T) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL is not set")
	}

	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse Redis URL: %v", err)
	}
	client := redis.NewClient(options)
	defer client.Close()

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping Redis: %v", err)
	}

	prefix := fmt.Sprintf(
		"test:http-signature:capability:%d:",
		time.Now().UnixNano(),
	)
	store, err := NewRedisDestinationCapabilityStore(
		client,
		prefix,
	)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	origin := "https://remote.example:8443"
	capability := DestinationCapability{
		Origin:     origin,
		Scope:      DestinationScopeDelivery,
		Profile:    ProfileRFC9421,
		Evidence:   CapabilityEvidenceSuccessfulRFC9421,
		ObservedAt: now,
		ExpiresAt:  now.Add(2 * time.Minute),
	}

	saved, err := store.SaveDestinationCapability(ctx, capability)
	if err != nil {
		t.Fatalf("save capability: %v", err)
	}
	if !saved {
		t.Fatal("first capability observation was not saved")
	}

	loaded, found, err := store.LoadDestinationCapability(
		ctx,
		DestinationScopeDelivery,
		origin,
	)
	if err != nil {
		t.Fatalf("load capability: %v", err)
	}
	if !found {
		t.Fatal("saved capability was not found")
	}
	if loaded != capability {
		t.Fatalf("loaded capability = %+v; want %+v", loaded, capability)
	}

	key := store.capabilityKey(
		DestinationScopeDelivery,
		origin,
	)
	if strings.Contains(key, "remote.example") ||
		strings.Contains(key, origin) {
		t.Fatalf("Redis key exposes raw origin: %q", key)
	}
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ttl <= 0 || ttl > 2*time.Minute {
		t.Fatalf("capability TTL = %s", ttl)
	}

	stale := capability
	stale.Profile = ProfileLegacy
	stale.Evidence = CapabilityEvidenceExplicitRFC9421Rejection
	stale.ObservedAt = now.Add(-time.Minute)
	stale.ExpiresAt = now.Add(time.Minute)
	saved, err = store.SaveDestinationCapability(ctx, stale)
	if err != nil {
		t.Fatalf("save stale capability: %v", err)
	}
	if saved {
		t.Fatal("stale capability observation overwrote newer state")
	}

	newer := stale
	newer.ObservedAt = now.Add(time.Millisecond)
	newer.ExpiresAt = now.Add(time.Minute)
	saved, err = store.SaveDestinationCapability(ctx, newer)
	if err != nil {
		t.Fatalf("save newer capability: %v", err)
	}
	if !saved {
		t.Fatal("newer capability observation was not saved")
	}
	loaded, found, err = store.LoadDestinationCapability(
		ctx,
		DestinationScopeDelivery,
		origin,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !found || loaded.Profile != ProfileLegacy ||
		loaded.Evidence !=
			CapabilityEvidenceExplicitRFC9421Rejection {
		t.Fatalf("newer capability was not loaded: %+v", loaded)
	}

	if err := client.Del(ctx, key).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisDestinationCapabilityStoreSeparatesScopes(
	t *testing.T,
) {
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL is not set")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()

	store, err := NewRedisDestinationCapabilityStore(
		client,
		fmt.Sprintf(
			"test:http-signature:scope:%d:",
			time.Now().UnixNano(),
		),
	)
	if err != nil {
		t.Fatal(err)
	}

	origin := "https://remote.example"
	fetchKey := store.capabilityKey(
		DestinationScopeFetch,
		origin,
	)
	deliveryKey := store.capabilityKey(
		DestinationScopeDelivery,
		origin,
	)
	if fetchKey == deliveryKey {
		t.Fatal("fetch and delivery capability keys are identical")
	}
}
