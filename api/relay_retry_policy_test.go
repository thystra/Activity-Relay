// File: api/relay_retry_policy_test.go
package api

import (
	"testing"

	"github.com/thystra/Activity-Relay/internal/deliverypolicy"
)

func TestRelayTaskUsesBoundedRetryPolicy(t *testing.T) {
	signature := relayTask(
		"https://receiver.example/inbox",
		"activity-storage-id",
	)

	if signature.RetryCount != deliverypolicy.RetryCount {
		t.Fatalf(
			"RetryCount = %d; want %d",
			signature.RetryCount,
			deliverypolicy.RetryCount,
		)
	}
	if signature.RetryTimeout != deliverypolicy.InitialRetryTimeoutSeconds {
		t.Fatalf(
			"RetryTimeout = %d; want %d",
			signature.RetryTimeout,
			deliverypolicy.InitialRetryTimeoutSeconds,
		)
	}
}

// EOF: api/relay_retry_policy_test.go
