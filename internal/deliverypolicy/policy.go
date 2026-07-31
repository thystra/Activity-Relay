// File: internal/deliverypolicy/policy.go
package deliverypolicy

import "time"

const (
	// RetryCount is the number of delayed attempts after the initial delivery.
	RetryCount = 5

	// InitialRetryTimeoutSeconds seeds Machinery's Fibonacci retry schedule.
	// Machinery advances 5 to 8 seconds for the first retry.
	InitialRetryTimeoutSeconds = 5

	// ActivityRetentionSeconds keeps the shared payload beyond the complete
	// retry horizon while remaining bounded.
	ActivityRetentionSeconds = 15 * 60

	MaxAttempts = RetryCount + 1
)

const ActivityRetention = time.Duration(ActivityRetentionSeconds) * time.Second

// EOF: internal/deliverypolicy/policy.go
