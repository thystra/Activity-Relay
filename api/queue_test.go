package api

import (
	"context"
	"testing"
)

func TestQueueCapacityReservationIsBounded(t *testing.T) {
	ctx := context.Background()
	if err := RelayState.RedisClient.Del(ctx, "relay", queueReservationKey).Err(); err != nil {
		t.Fatal(err)
	}

	maximum := int(GlobalConfig.MaxQueueJobs())
	if !reserveQueueCapacity(maximum) {
		t.Fatal("expected reservation up to configured queue maximum to succeed")
	}
	defer releaseQueueCapacity(maximum)
	if reserveQueueCapacity(1) {
		releaseQueueCapacity(1)
		t.Fatal("expected reservation beyond configured queue maximum to fail")
	}
}
