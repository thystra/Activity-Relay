package deliver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/thystra/Activity-Relay/internal/observability"
)

func TestRelayActivityRecordsOperationalMetric(t *testing.T) {
	if err := RedisClient.Del(context.Background(), observability.OperationalMetricsKey).Err(); err != nil {
		t.Fatal(err)
	}
	previous := OperationalMetrics
	OperationalMetrics = observability.NewRecorder(RedisClient)
	defer func() { OperationalMetrics = previous }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	activityID := uuid.NewString()
	if err := RedisClient.HSet(context.Background(), "relay:activity:"+activityID, map[string]interface{}{
		"body": "ExampleData", "remain_count": 1,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := relayActivityV2(server.URL, activityID); err == nil {
		t.Fatal("expected delivery failure")
	}
	ledger, err := RedisClient.HGetAll(context.Background(), observability.OperationalMetricsKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for field, value := range ledger {
		if strings.HasPrefix(field, "delivery_attempts_total|failure|http_5xx") && value == "1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("delivery metric missing: %#v", ledger)
	}
}
