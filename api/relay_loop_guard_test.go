// File: api/relay_loop_guard_test.go

package api

import (
	"context"
	"strings"
	"testing"

	"github.com/thystra/Activity-Relay/internal/deliverypolicy"
	"github.com/thystra/Activity-Relay/models"
)

func clearCanonicalRelayTestKeys(t *testing.T) {
	t.Helper()
	keys, err := models.ScanKeys(
		context.Background(),
		RelayState.RedisClient,
		canonicalRelayKeyPrefix+"*",
	)
	if err != nil {
		t.Fatalf("scan canonical relay keys: %v", err)
	}
	if len(keys) > 0 {
		if err := RelayState.RedisClient.Del(
			context.Background(),
			keys...,
		).Err(); err != nil {
			t.Fatalf("clear canonical relay keys: %v", err)
		}
	}
}

func TestCanonicalRelayKeyDoesNotExposeCanonicalURL(t *testing.T) {
	canonicalID := "https://origin.example/activities/create-1"
	key := canonicalRelayKey(canonicalID)

	if !strings.HasPrefix(key, canonicalRelayKeyPrefix) {
		t.Fatalf("canonical key = %q; missing expected prefix", key)
	}
	if strings.Contains(key, canonicalID) || strings.Contains(key, "origin.example") {
		t.Fatalf("canonical key exposes the source identifier: %q", key)
	}
	if got, want := len(strings.TrimPrefix(key, canonicalRelayKeyPrefix)), 64; got != want {
		t.Fatalf("canonical key digest length = %d; want %d", got, want)
	}
}

func TestCanonicalRelayReservationIsAtomicAndBounded(t *testing.T) {
	clearCanonicalRelayTestKeys(t)
	t.Cleanup(func() {
		clearCanonicalRelayTestKeys(t)
	})

	canonicalID := "https://origin.example/activities/create-1"
	first, accepted, err := reserveCanonicalRelayActivity(canonicalID)
	if err != nil {
		t.Fatalf("reserve canonical activity: %v", err)
	}
	if !accepted {
		t.Fatal("first canonical reservation was not accepted")
	}

	_, accepted, err = reserveCanonicalRelayActivity(canonicalID)
	if err != nil {
		t.Fatalf("repeat canonical reservation: %v", err)
	}
	if accepted {
		t.Fatal("duplicate canonical reservation was accepted")
	}

	ttl, err := RelayState.RedisClient.TTL(
		context.Background(),
		first.key,
	).Result()
	if err != nil {
		t.Fatalf("read canonical reservation TTL: %v", err)
	}
	if ttl <= 0 || ttl > deliverypolicy.ActivityRetention {
		t.Fatalf(
			"canonical reservation TTL = %s; want > 0 and <= %s",
			ttl,
			deliverypolicy.ActivityRetention,
		)
	}

	wrong := first
	wrong.token = "not-the-owner"
	releaseCanonicalRelayActivity(wrong)
	exists, err := RelayState.RedisClient.Exists(
		context.Background(),
		first.key,
	).Result()
	if err != nil {
		t.Fatalf("check canonical reservation after wrong release: %v", err)
	}
	if exists != 1 {
		t.Fatal("non-owner released the canonical reservation")
	}

	releaseCanonicalRelayActivity(first)
	exists, err = RelayState.RedisClient.Exists(
		context.Background(),
		first.key,
	).Result()
	if err != nil {
		t.Fatalf("check canonical reservation after release: %v", err)
	}
	if exists != 0 {
		t.Fatal("owner did not release the canonical reservation")
	}
}

// EOF: api/relay_loop_guard_test.go
