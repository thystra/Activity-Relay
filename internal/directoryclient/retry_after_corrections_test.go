package directoryclient

import (
	"net/http"
	"testing"
	"time"
)

func TestBoundedRetryAfterAlignsDeltaDateAndCap(t *testing.T) {
	now := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		code   ErrorCode
		status int
		value  string
		want   time.Duration
		valid  bool
	}{
		{name: "rate delta", code: ErrorRateLimited, status: http.StatusTooManyRequests, value: "7200", want: 2 * time.Hour, valid: true},
		{name: "internal date", code: ErrorInternal, status: http.StatusServiceUnavailable, value: now.Add(3 * time.Hour).Format(http.TimeFormat), want: 3 * time.Hour, valid: true},
		{name: "cap", code: ErrorRateLimited, status: http.StatusTooManyRequests, value: "172800", want: MaximumRetryAfter, valid: true},
		{name: "missing", code: ErrorRateLimited, status: http.StatusTooManyRequests, value: "", want: 0, valid: true},
		{name: "zero", code: ErrorRateLimited, status: http.StatusTooManyRequests, value: "0", want: 0, valid: true},
		{name: "past", code: ErrorRateLimited, status: http.StatusTooManyRequests, value: now.Add(-time.Minute).Format(http.TimeFormat), valid: false},
		{name: "malformed", code: ErrorRateLimited, status: http.StatusTooManyRequests, value: "later", valid: false},
		{name: "nonretryable lifecycle", code: ErrorLifecycleUnavailable, status: http.StatusServiceUnavailable, value: "7200", want: 0, valid: true},
		{name: "internal 500", code: ErrorInternal, status: http.StatusInternalServerError, value: "7200", want: 0, valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := make(http.Header)
			if test.value != "" {
				header.Set("Retry-After", test.value)
			}
			got, err := boundedRetryAfter(test.code, test.status, header, now)
			if test.valid && err != nil {
				t.Fatalf("boundedRetryAfter() error = %v", err)
			}
			if !test.valid && err == nil {
				t.Fatal("boundedRetryAfter() accepted invalid value")
			}
			if test.valid && got != test.want {
				t.Fatalf("boundedRetryAfter() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestDecodeProtocolErrorUsesInjectedClockForHTTPDate(t *testing.T) {
	now := time.Date(2026, 8, 5, 16, 0, 0, 0, time.UTC)
	header := http.Header{
		"Retry-After":  []string{now.Add(90 * time.Minute).Format(http.TimeFormat)},
		"Content-Type": []string{"application/json"},
	}
	err := decodeProtocolErrorAt(
		http.StatusTooManyRequests,
		header,
		[]byte(`{"protocol_version":1,"error":{"code":"rate_limited","message":"bounded"}}`),
		now,
	)
	protocolError, ok := err.(*ProtocolError)
	if !ok || protocolError.RetryAfter != 90*time.Minute {
		t.Fatalf("decodeProtocolErrorAt() = %#v", err)
	}
}
