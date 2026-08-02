// File: api/relay_loop_guard.go

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/deliverypolicy"
)

const canonicalRelayKeyPrefix = "relay:canonical:"

const releaseCanonicalRelayScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`

type canonicalRelayReservation struct {
	key   string
	token string
}

func canonicalRelayKey(canonicalID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(canonicalID)))
	return canonicalRelayKeyPrefix + hex.EncodeToString(sum[:])
}

func reserveCanonicalRelayActivity(
	canonicalID string,
) (canonicalRelayReservation, bool, error) {
	canonicalID = strings.TrimSpace(canonicalID)
	if canonicalID == "" {
		return canonicalRelayReservation{}, false, errors.New(
			"canonical relay activity ID is empty",
		)
	}

	reservation := canonicalRelayReservation{
		key:   canonicalRelayKey(canonicalID),
		token: uuid.NewString(),
	}
	accepted, err := RelayState.RedisClient.SetNX(
		context.Background(),
		reservation.key,
		reservation.token,
		deliverypolicy.ActivityRetention,
	).Result()
	if err != nil {
		recordRedisOperationFailure("api", "canonical_reserve")
		return canonicalRelayReservation{}, false, err
	}
	return reservation, accepted, nil
}

func releaseCanonicalRelayActivity(
	reservation canonicalRelayReservation,
) {
	if reservation.key == "" || reservation.token == "" {
		return
	}
	if err := RelayState.RedisClient.Eval(
		context.Background(),
		releaseCanonicalRelayScript,
		[]string{reservation.key},
		reservation.token,
	).Err(); err != nil {
		recordRedisOperationFailure("api", "canonical_release")
		logrus.WithError(err).Error(
			"Unable to release canonical relay reservation",
		)
	}
}

// EOF: api/relay_loop_guard.go
