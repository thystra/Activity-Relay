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

func TestRedisRFC9421NonceStoreReservesAtomically(t *testing.T) {
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
		"test:rfc9421:nonce:%d:",
		time.Now().UnixNano(),
	)
	store, err := NewRedisRFC9421NonceStore(client, prefix)
	if err != nil {
		t.Fatalf("create Redis nonce store: %v", err)
	}

	keyID := "https://relay.example/actor#main-key"
	nonce := "nonce-that-must-not-appear-in-the-key"
	ttl := 2 * time.Minute

	reserved, err := store.ReserveRFC9421Nonce(
		ctx,
		keyID,
		nonce,
		ttl,
	)
	if err != nil {
		t.Fatalf("first nonce reservation: %v", err)
	}
	if !reserved {
		t.Fatal("first nonce reservation was not accepted")
	}

	reserved, err = store.ReserveRFC9421Nonce(
		ctx,
		keyID,
		nonce,
		ttl,
	)
	if err != nil {
		t.Fatalf("second nonce reservation: %v", err)
	}
	if reserved {
		t.Fatal("duplicate nonce reservation was accepted")
	}

	keys, err := client.Keys(ctx, prefix+"*").Result()
	if err != nil {
		t.Fatalf("list nonce keys: %v", err)
	}
	defer func() {
		if len(keys) != 0 {
			_ = client.Del(ctx, keys...).Err()
		}
	}()

	if len(keys) != 1 {
		t.Fatalf("nonce key count = %d; want 1", len(keys))
	}
	if strings.Contains(keys[0], keyID) || strings.Contains(keys[0], nonce) {
		t.Fatalf("nonce key exposes raw identity material: %q", keys[0])
	}
	remaining, err := client.TTL(ctx, keys[0]).Result()
	if err != nil {
		t.Fatalf("read nonce TTL: %v", err)
	}
	if remaining <= 0 || remaining > ttl {
		t.Fatalf("nonce TTL = %s; expected positive and <= %s", remaining, ttl)
	}
}
