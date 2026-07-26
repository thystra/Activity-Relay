package api

import (
	"context"
	"testing"
	"time"
)

func TestStoreRelayActivityReturnsSuccessAndPersistsPayload(t *testing.T) {
	const activityID = "store-regression-test"
	key := "relay:activity:" + activityID
	ctx := context.Background()

	RelayState.RedisClient.Del(ctx, key)
	t.Cleanup(func() {
		RelayState.RedisClient.Del(ctx, key)
	})

	body := []byte(`{"type":"Create","id":"https://publisher.example/activities/1"}`)
	if err := storeRelayActivity(activityID, body, 2); err != nil {
		t.Fatalf("storeRelayActivity returned an error: %v", err)
	}

	values, err := RelayState.RedisClient.HGetAll(ctx, key).Result()
	if err != nil {
		t.Fatalf("read stored relay activity: %v", err)
	}
	if values["body"] != string(body) {
		t.Errorf("stored body = %q; want %q", values["body"], string(body))
	}
	if values["remain_count"] != "2" {
		t.Errorf("remain_count = %q; want 2", values["remain_count"])
	}

	ttl, err := RelayState.RedisClient.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read relay activity TTL: %v", err)
	}
	if ttl <= 0 || ttl > 2*time.Minute {
		t.Errorf("TTL = %v; want > 0 and <= 2m", ttl)
	}
}
