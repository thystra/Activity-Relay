// File: deliver/retry_test.go
package deliver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RichardKnop/machinery/v2/tasks"
	"github.com/google/uuid"
	"github.com/thystra/Activity-Relay/internal/deliverypolicy"
	"github.com/thystra/Activity-Relay/models"
)

func storeRetryTestActivity(t *testing.T, body string, remainCount int) string {
	t.Helper()

	activityID := uuid.NewString()
	key := "relay:activity:" + activityID
	err := RedisClient.HSet(
		context.Background(),
		key,
		"body",
		body,
		"remain_count",
		remainCount,
	).Err()
	if err != nil {
		t.Fatalf("store retry test activity: %v", err)
	}
	if err := RedisClient.Expire(
		context.Background(),
		key,
		deliverypolicy.ActivityRetention,
	).Err(); err != nil {
		t.Fatalf("expire retry test activity: %v", err)
	}
	t.Cleanup(func() {
		RedisClient.Del(context.Background(), key)
	})
	return activityID
}

func callRelayTaskForTest(
	t *testing.T,
	inboxURL string,
	activityID string,
	retriesRemaining int,
	retryTimeout int,
) error {
	t.Helper()

	signature := &tasks.Signature{
		UUID:         "task_" + uuid.NewString(),
		Name:         "relay-v2",
		RetryCount:   retriesRemaining,
		RetryTimeout: retryTimeout,
		Args: []tasks.Arg{
			{Name: "inboxURL", Type: "string", Value: inboxURL},
			{Name: "activityID", Type: "string", Value: activityID},
		},
	}
	task, err := tasks.NewWithSignature(relayActivityV2, signature)
	if err != nil {
		t.Fatalf("create relay task: %v", err)
	}
	_, err = task.Call()
	return err
}

func TestRetriableFailurePreservesBodyUntilSuccessfulRetry(t *testing.T) {
	RedisClient.FlushAll(context.Background()).Result()

	status := http.StatusInternalServerError
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte("temporary receiver failure"))
	}))
	defer server.Close()

	body := `{
		"id":"https://origin.example/activities/retry-test",
		"type":"Announce",
		"actor":"https://origin.example/actor",
		"object":"https://origin.example/objects/retry-test"
	}`
	activityID := storeRetryTestActivity(t, body, 1)
	key := "relay:activity:" + activityID

	err := callRelayTaskForTest(
		t,
		server.URL,
		activityID,
		deliverypolicy.RetryCount,
		deliverypolicy.InitialRetryTimeoutSeconds,
	)
	if err == nil {
		t.Fatal("initial failure returned nil")
	}

	values, err := RedisClient.HGetAll(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("read retained activity: %v", err)
	}
	if values["body"] != body {
		t.Fatalf("activity body was not retained after retriable failure")
	}
	if values["remain_count"] != "1" {
		t.Fatalf(
			"remain_count after retriable failure = %q; want 1",
			values["remain_count"],
		)
	}

	status = http.StatusAccepted
	err = callRelayTaskForTest(
		t,
		server.URL,
		activityID,
		deliverypolicy.RetryCount-1,
		8,
	)
	if err != nil {
		t.Fatalf("successful retry returned error: %v", err)
	}

	exists, err := RedisClient.Exists(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("check successful retry cleanup: %v", err)
	}
	if exists != 0 {
		t.Fatal("activity body remained after terminal success")
	}

	domain, err := models.ReceiverDomainFromInboxURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	health, err := models.LoadReceiverDeliveryHealth(
		context.Background(),
		RedisClient,
		[]string{domain},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := health[domain]
	if got.TotalFailures != 1 || got.TotalSuccesses != 1 || got.ConsecutiveFailures != 0 {
		t.Fatalf("unexpected retry health: %+v", got)
	}
}

func TestFinalFailureReducesTargetCount(t *testing.T) {
	RedisClient.FlushAll(context.Background()).Result()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "terminal receiver failure", http.StatusBadGateway)
	}))
	defer server.Close()

	activityID := storeRetryTestActivity(
		t,
		`{"id":"https://origin.example/activities/final","type":"Announce"}`,
		1,
	)

	err := callRelayTaskForTest(t, server.URL, activityID, 0, 55)
	if err == nil {
		t.Fatal("terminal failure returned nil")
	}

	exists, err := RedisClient.Exists(
		context.Background(),
		"relay:activity:"+activityID,
	).Result()
	if err != nil {
		t.Fatalf("check terminal cleanup: %v", err)
	}
	if exists != 0 {
		t.Fatal("activity body remained after final exhausted failure")
	}
}

func TestDeliveryAttemptNumbering(t *testing.T) {
	for retries, wantAttempt := range map[int]int{
		deliverypolicy.RetryCount:     1,
		deliverypolicy.RetryCount - 1: 2,
		0:                             deliverypolicy.MaxAttempts,
	} {
		signature := &tasks.Signature{
			UUID:         fmt.Sprintf("task-attempt-%d", retries),
			RetryCount:   retries,
			RetryTimeout: deliverypolicy.InitialRetryTimeoutSeconds,
		}
		task, err := tasks.NewWithSignature(
			func(ctx context.Context) error {
				got := currentDeliveryAttempt(ctx)
				if got.Attempt != wantAttempt {
					t.Fatalf(
						"retries remaining %d: attempt = %d; want %d",
						retries,
						got.Attempt,
						wantAttempt,
					)
				}
				return nil
			},
			signature,
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := task.Call(); err != nil {
			t.Fatal(err)
		}
	}
}

// EOF: deliver/retry_test.go
