package httpsignature

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultDestinationCapabilityPrefix = "relay:http-signature:capability:"

const saveDestinationCapabilityScript = `
local current = redis.call("HGET", KEYS[1], "observed_at_ms")
if current and tonumber(current) >= tonumber(ARGV[5]) then
  return 0
end
redis.call(
  "HSET",
  KEYS[1],
  "origin", ARGV[1],
  "scope", ARGV[2],
  "profile", ARGV[3],
  "evidence", ARGV[4],
  "observed_at_ms", ARGV[5],
  "expires_at_ms", ARGV[6]
)
redis.call("PEXPIREAT", KEYS[1], ARGV[6])
return 1
`

// RedisDestinationCapabilityStore persists bounded destination observations.
// Redis key names contain only a SHA-256 digest of scope and normalized origin.
type RedisDestinationCapabilityStore struct {
	client *redis.Client
	prefix string
}

func NewRedisDestinationCapabilityStore(
	client *redis.Client,
	prefix string,
) (*RedisDestinationCapabilityStore, error) {
	if client == nil {
		return nil, errors.New(
			"destination capability Redis client is nil",
		)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = defaultDestinationCapabilityPrefix
	}
	return &RedisDestinationCapabilityStore{
		client: client,
		prefix: prefix,
	}, nil
}

func (store *RedisDestinationCapabilityStore) capabilityKey(
	scope DestinationScope,
	origin string,
) string {
	sum := sha256.Sum256(
		[]byte(string(scope) + "\x00" + origin),
	)
	return store.prefix + hex.EncodeToString(sum[:])
}

func (store *RedisDestinationCapabilityStore) SaveDestinationCapability(
	ctx context.Context,
	capability DestinationCapability,
) (bool, error) {
	if store == nil || store.client == nil {
		return false, errors.New(
			"destination capability Redis store is not initialized",
		)
	}
	if err := capability.Validate(); err != nil {
		return false, err
	}
	if !capability.ExpiresAt.After(time.Now()) {
		return false, errors.New(
			"destination capability is already expired",
		)
	}

	result, err := store.client.Eval(
		ctx,
		saveDestinationCapabilityScript,
		[]string{
			store.capabilityKey(
				capability.Scope,
				capability.Origin,
			),
		},
		capability.Origin,
		string(capability.Scope),
		string(capability.Profile),
		string(capability.Evidence),
		capability.ObservedAt.UnixMilli(),
		capability.ExpiresAt.UnixMilli(),
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func parseCapabilityTime(
	values map[string]string,
	field string,
) (time.Time, error) {
	raw := values[field]
	if raw == "" {
		return time.Time{}, fmt.Errorf(
			"destination capability field %q is empty",
			field,
		)
	}
	milliseconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"parse destination capability field %q: %w",
			field,
			err,
		)
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func (store *RedisDestinationCapabilityStore) LoadDestinationCapability(
	ctx context.Context,
	scope DestinationScope,
	origin string,
) (DestinationCapability, bool, error) {
	if store == nil || store.client == nil {
		return DestinationCapability{}, false, errors.New(
			"destination capability Redis store is not initialized",
		)
	}
	if err := scope.Validate(); err != nil {
		return DestinationCapability{}, false, err
	}
	normalizedOrigin, err := NormalizeDestinationOrigin(origin)
	if err != nil {
		return DestinationCapability{}, false, err
	}
	if normalizedOrigin != origin {
		return DestinationCapability{}, false, errors.New(
			"destination capability lookup origin is not normalized",
		)
	}

	values, err := store.client.HGetAll(
		ctx,
		store.capabilityKey(scope, origin),
	).Result()
	if err != nil {
		return DestinationCapability{}, false, err
	}
	if len(values) == 0 {
		return DestinationCapability{}, false, nil
	}

	observedAt, err := parseCapabilityTime(
		values,
		"observed_at_ms",
	)
	if err != nil {
		return DestinationCapability{}, false, err
	}
	expiresAt, err := parseCapabilityTime(
		values,
		"expires_at_ms",
	)
	if err != nil {
		return DestinationCapability{}, false, err
	}

	capability := DestinationCapability{
		Origin:     values["origin"],
		Scope:      DestinationScope(values["scope"]),
		Profile:    Profile(values["profile"]),
		Evidence:   CapabilityEvidence(values["evidence"]),
		ObservedAt: observedAt,
		ExpiresAt:  expiresAt,
	}
	if capability.Origin != origin ||
		capability.Scope != scope {
		return DestinationCapability{}, false, errors.New(
			"stored destination capability identity does not match key",
		)
	}
	if err := capability.Validate(); err != nil {
		return DestinationCapability{}, false, err
	}
	return capability, true, nil
}
