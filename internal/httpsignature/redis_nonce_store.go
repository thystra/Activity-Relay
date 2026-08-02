package httpsignature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultRFC9421NoncePrefix = "relay:http-signature:nonce:"

type redisSetNXClient interface {
	SetNX(
		context.Context,
		string,
		interface{},
		time.Duration,
	) *redis.BoolCmd
}

// RedisRFC9421NonceStore atomically stores bounded replay markers without
// exposing raw key IDs or nonce values in Redis key names.
type RedisRFC9421NonceStore struct {
	client redisSetNXClient
	prefix string
}

// NewRedisRFC9421NonceStore validates and returns a Redis nonce store.
func NewRedisRFC9421NonceStore(
	client redisSetNXClient,
	prefix string,
) (*RedisRFC9421NonceStore, error) {
	if client == nil {
		return nil, errors.New("RFC 9421 Redis nonce client is nil")
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = defaultRFC9421NoncePrefix
	}
	return &RedisRFC9421NonceStore{
		client: client,
		prefix: prefix,
	}, nil
}

func (store *RedisRFC9421NonceStore) nonceKey(
	keyID string,
	nonce string,
) string {
	sum := sha256.Sum256([]byte(keyID + "\x00" + nonce))
	return store.prefix + hex.EncodeToString(sum[:])
}

// ReserveRFC9421Nonce reserves one key-ID/nonce pair with SET NX and a TTL.
func (store *RedisRFC9421NonceStore) ReserveRFC9421Nonce(
	ctx context.Context,
	keyID string,
	nonce string,
	ttl time.Duration,
) (bool, error) {
	if store == nil || store.client == nil {
		return false, errors.New("RFC 9421 Redis nonce store is not initialized")
	}
	if strings.TrimSpace(keyID) == "" {
		return false, errors.New("RFC 9421 nonce reservation key ID is empty")
	}
	if nonce == "" {
		return false, errors.New("RFC 9421 nonce reservation nonce is empty")
	}
	if ttl <= 0 {
		return false, errors.New("RFC 9421 nonce reservation TTL is not positive")
	}

	return store.client.SetNX(
		ctx,
		store.nonceKey(keyID, nonce),
		"1",
		ttl,
	).Result()
}
