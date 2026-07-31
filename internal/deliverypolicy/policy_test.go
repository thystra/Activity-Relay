// File: internal/deliverypolicy/policy_test.go
package deliverypolicy

import (
	"testing"
	"time"

	"github.com/RichardKnop/machinery/v2/retry"
)

func TestRetentionExceedsCompleteRetryHorizon(t *testing.T) {
	timeout := InitialRetryTimeoutSeconds
	var horizon time.Duration

	for range RetryCount {
		timeout = retry.FibonacciNext(timeout)
		horizon += time.Duration(timeout) * time.Second
	}

	if horizon <= 2*time.Minute {
		t.Fatalf("retry horizon = %v; want greater than the former 2m payload TTL", horizon)
	}
	if ActivityRetention < horizon+5*time.Minute {
		t.Fatalf(
			"activity retention = %v; want at least retry horizon %v plus 5m margin",
			ActivityRetention,
			horizon,
		)
	}
}

// EOF: internal/deliverypolicy/policy_test.go
