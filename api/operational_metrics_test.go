package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/thystra/Activity-Relay/internal/observability"
	"github.com/thystra/Activity-Relay/models"
)

func TestClassifyInboxMetric(t *testing.T) {
	tests := []struct {
		status int
		result string
		reason string
	}{
		{http.StatusAccepted, "accepted", "accepted"},
		{http.StatusBadRequest, "rejected", "invalid"},
		{http.StatusUnauthorized, "rejected", "policy"},
		{http.StatusMethodNotAllowed, "ignored", "method"},
		{http.StatusInternalServerError, "rejected", "internal"},
	}
	for _, test := range tests {
		result, reason := classifyInboxMetric(test.status)
		if result != test.result || reason != test.reason {
			t.Errorf("classifyInboxMetric(%d) = %q/%q; want %q/%q", test.status, result, reason, test.result, test.reason)
		}
	}
}

func TestHandleInboxRecordsRejectedActivity(t *testing.T) {
	ctx := context.Background()
	if err := RelayState.RedisClient.Del(ctx, observability.OperationalMetricsKey).Err(); err != nil {
		t.Fatal(err)
	}
	previous := OperationalMetrics
	OperationalMetrics = observability.NewRecorder(RelayState.RedisClient)
	defer func() { OperationalMetrics = previous }()

	request := httptest.NewRequest(http.MethodPost, "/inbox", strings.NewReader("{}"))
	response := httptest.NewRecorder()
	handleInbox(response, request, func(*http.Request) (*models.Activity, *models.Actor, []byte, error) {
		return nil, nil, nil, errors.New("invalid activity")
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusBadRequest)
	}
	ledger, err := RelayState.RedisClient.HGetAll(ctx, observability.OperationalMetricsKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ledger["activities_total|rejected|other|invalid"] != "1" {
		t.Fatalf("rejected activity metric missing: %#v", ledger)
	}
}

func TestEnqueueActivityRecordsNoTargets(t *testing.T) {
	ctx := context.Background()
	if err := RelayState.RedisClient.Del(ctx, observability.OperationalMetricsKey).Err(); err != nil {
		t.Fatal(err)
	}
	previous := OperationalMetrics
	OperationalMetrics = observability.NewRecorder(RelayState.RedisClient)
	defer func() { OperationalMetrics = previous }()

	enqueueActivity(nil, "source.example", []byte(`{"type":"Announce"}`))
	ledger, err := RelayState.RedisClient.HGetAll(ctx, observability.OperationalMetricsKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if ledger["queue_admissions_total|relay|skipped|no_targets"] != "1" {
		t.Fatalf("no-target queue metric missing: %#v", ledger)
	}
}
