package models

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestReceiverDomainFromInboxURL(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
		wantErr bool
	}{
		{name: "hostname", address: "https://EXAMPLE.COM./inbox", want: "example.com"},
		{name: "non-default port", address: "https://Example.COM.:8443/inbox", want: "example.com:8443"},
		{name: "IPv6 port", address: "http://[2001:db8::1]:8080/inbox", want: "[2001:db8::1]:8080"},
		{name: "missing host", address: "https:///inbox", wantErr: true},
		{name: "unsupported scheme", address: "file:///tmp/inbox", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ReceiverDomainFromInboxURL(test.address)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got domain %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("domain = %q; want %q", got, test.want)
			}
		})
	}
}

func TestRecordAndLoadReceiverDeliveryHealth(t *testing.T) {
	ctx := context.Background()
	client := relayState.RedisClient
	client.FlushAll(ctx)
	firstFailure := time.Date(2026, 7, 29, 20, 0, 0, 0, time.UTC)
	secondFailure := firstFailure.Add(time.Minute)
	success := secondFailure.Add(time.Minute)

	if err := RecordReceiverDelivery(ctx, client, "Example.COM.", false, firstFailure); err != nil {
		t.Fatal(err)
	}
	if err := RecordReceiverDelivery(ctx, client, "example.com", false, secondFailure); err != nil {
		t.Fatal(err)
	}
	if err := RecordReceiverDelivery(ctx, client, "example.com", true, success); err != nil {
		t.Fatal(err)
	}

	health, err := LoadReceiverDeliveryHealth(ctx, client, []string{"EXAMPLE.COM.", "missing.example", "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(health) != 2 {
		t.Fatalf("health records = %d; want 2", len(health))
	}
	got := health["example.com"]
	if got.LastFailureAt != secondFailure.Format(time.RFC3339) {
		t.Errorf("last failure = %q; want %q", got.LastFailureAt, secondFailure.Format(time.RFC3339))
	}
	if got.LastSuccessAt != success.Format(time.RFC3339) {
		t.Errorf("last success = %q; want %q", got.LastSuccessAt, success.Format(time.RFC3339))
	}
	if got.ConsecutiveFailures != 0 || got.TotalFailures != 2 || got.TotalSuccesses != 1 {
		t.Errorf("unexpected counters: %+v", got)
	}
	missing := health["missing.example"]
	if missing.Domain != "missing.example" || missing.LastSuccessAt != "" ||
		missing.LastFailureAt != "" || missing.ConsecutiveFailures != 0 ||
		missing.TotalSuccesses != 0 || missing.TotalFailures != 0 {
		t.Errorf("unexpected empty-domain health: %+v", missing)
	}
	if ttl := client.TTL(ctx, receiverHealthKeyPrefix+"example.com").Val(); ttl != -1 {
		t.Errorf("receiver health TTL = %v; want no expiry", ttl)
	}
}

func TestRecordReceiverDeliveryHealthAtomicFailures(t *testing.T) {
	ctx := context.Background()
	client := relayState.RedisClient
	client.FlushAll(ctx)
	const attempts = 24
	var wait sync.WaitGroup
	errors := make(chan error, attempts)
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- RecordReceiverDelivery(ctx, client, "atomic.example", false, time.Time{})
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	health, err := LoadReceiverDeliveryHealth(ctx, client, []string{"atomic.example"})
	if err != nil {
		t.Fatal(err)
	}
	got := health["atomic.example"]
	if got.ConsecutiveFailures != attempts || got.TotalFailures != attempts || got.TotalSuccesses != 0 {
		t.Fatalf("unexpected atomic counters: %+v", got)
	}
}

func TestLoadReceiverDeliveryHealthRejectsMalformedCounter(t *testing.T) {
	ctx := context.Background()
	client := relayState.RedisClient
	client.FlushAll(ctx)
	if err := client.HSet(ctx, receiverHealthKeyPrefix+"bad.example", "total_failures", "not-a-number").Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReceiverDeliveryHealth(ctx, client, []string{"bad.example"}); err == nil {
		t.Fatal("expected malformed receiver counter error")
	}
}
