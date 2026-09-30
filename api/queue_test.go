package api

import (
	"context"
	"testing"

	"github.com/thystra/Activity-Relay/models"
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

func TestEnqueueActivitySkipsInvalidTargetAndQueuesHealthyTarget(t *testing.T) {
	ctx := context.Background()
	if err := RelayState.RedisClient.Del(ctx, "relay", queueReservationKey).Err(); err != nil {
		t.Fatal(err)
	}
	keys, err := models.ScanKeys(ctx, RelayState.RedisClient, "relay:activity:*")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) > 0 {
		if err := RelayState.RedisClient.Del(ctx, keys...).Err(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = RelayState.RedisClient.Del(ctx, "relay", queueReservationKey).Err()
		activityKeys, _ := models.ScanKeys(ctx, RelayState.RedisClient, "relay:activity:*")
		if len(activityKeys) > 0 {
			_ = RelayState.RedisClient.Del(ctx, activityKeys...).Err()
		}
	})

	targets := []models.Subscriber{
		{
			Domain:   "orphan.example",
			InboxURL: "",
		},
		{
			Domain:   "friendica.example",
			InboxURL: "https://friendica.example/friendica/inbox",
		},
	}
	if !enqueueActivityExcept(targets, []byte(`{"type":"Announce"}`)) {
		t.Fatal("expected healthy target to remain queueable after invalid target is skipped")
	}

	queued, err := RelayState.RedisClient.LLen(ctx, "relay").Result()
	if err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("queued relay tasks = %d; want 1", queued)
	}
	keys, err = models.ScanKeys(ctx, RelayState.RedisClient, "relay:activity:*")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("stored relay activities = %d; want 1", len(keys))
	}
	remainCount, err := RelayState.RedisClient.HGet(ctx, keys[0], "remain_count").Result()
	if err != nil {
		t.Fatal(err)
	}
	if remainCount != "1" {
		t.Fatalf("remain_count = %q; want 1", remainCount)
	}
}
