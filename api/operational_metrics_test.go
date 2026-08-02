package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
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

	enqueueActivity(nil, "source.example", []byte(`{"type":"Announce"}`))
	ledger, err := RelayState.RedisClient.HGetAll(ctx, observability.OperationalMetricsKey).Result()
	if err != nil {
		t.Fatal(err)
	}
	if value := ledger["queue_admissions_total|relay|skipped|no_targets"]; value == "" || value == "0" {
		t.Fatalf("no-target queue metric missing: %#v", ledger)
	}
}

func TestClassifyHTTPSignatureVerificationError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "accepted"},
		{relayhttpsig.ErrRFC9421Replay, "replay"},
		{errors.New("Content-Digest mismatch"), "digest"},
		{errors.New("signature is older than allowed"), "time"},
		{errors.New("request authority mismatch"), "authority"},
		{errors.New("public-key owner mismatch"), "actor"},
		{errors.New("resolve RFC 9421 key"), "key"},
		{errors.New("reserve RFC 9421 nonce: Redis unavailable"), "redis"},
		{errors.New("required covered component is missing"), "policy"},
		{errors.New("parse RFC 9421 signature fields"), "parse"},
		{errors.New("verify RFC 9421 signature"), "crypto"},
		{errors.New("invalid character in activity"), "activity"},
		{errors.New("opaque"), "other"},
	}
	for _, test := range tests {
		if got := classifyHTTPSignatureVerificationError(test.err); got != test.want {
			t.Errorf(
				"classifyHTTPSignatureVerificationError(%v) = %q; want %q",
				test.err,
				got,
				test.want,
			)
		}
	}
}
