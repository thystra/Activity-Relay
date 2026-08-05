package observability

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func operationalTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		t.Skip("REDIS_URL is required")
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestOperationalRecorderAndCollector(t *testing.T) {
	client := operationalTestRedis(t)
	recorder := NewRecorder(client)
	ctx := context.Background()
	if err := recorder.RecordActivity(ctx, "accepted", "Create", "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordActivity(ctx, "invented", "PrivateType", "raw error text"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordHTTPSignatureVerification(
		ctx,
		"rfc9421",
		"failure",
		"replay",
	); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordQueueAdmission(ctx, "relay", "accepted", "accepted"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordFanoutTargets(ctx, "queued", 3); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordDelivery(ctx, "failure", "http_5xx"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordDirectory(ctx, "failure", "rate_limited"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordDirectory(ctx, "private-origin.example", "https://directory.example/path"); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordRedisFailure(ctx, "worker", "receiver_health"); err != nil {
		t.Fatal(err)
	}
	if err := client.RPush(ctx, "relay", "one", "two").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "relay:queue:reservations", 4, 0).Err(); err != nil {
		t.Fatal(err)
	}

	collector := NewOperationalCollector(client, func(context.Context) (RuntimeSnapshot, error) {
		return RuntimeSnapshot{
			Subscribers: 2, Followers: 1, UniqueReceivers: 2, Publishers: 3,
			HealthyReceivers: 1, FailingReceivers: 1,
		}, nil
	})
	registry := prometheus.NewRegistry()
	if err := registry.Register(collector); err != nil {
		t.Fatal(err)
	}
	recorderHTTP := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(recorderHTTP, httptest.NewRequest("GET", "/metrics", nil))
	body := recorderHTTP.Body.String()
	for _, expected := range []string{
		`activity_relay_activities_total{reason="accepted",result="accepted",type="Create"} 1`,
		`activity_relay_activities_total{reason="other",result="other",type="other"} 1`,
		`activity_relay_http_signature_verifications_total{profile="rfc9421",reason="replay",result="failure"} 1`,
		`activity_relay_queue_admissions_total{kind="relay",reason="accepted",result="accepted"} 1`,
		`activity_relay_fanout_targets_total{result="queued"} 3`,
		`activity_relay_delivery_attempts_total{error_class="http_5xx",result="failure"} 1`,
		`activity_relay_directory_scheduler_attempts_total{diagnostic="rate_limited",result="failure"} 1`,
		`activity_relay_directory_scheduler_attempts_total{diagnostic="other",result="other"} 1`,
		`activity_relay_redis_operation_failures_total{component="worker",operation="receiver_health"} 1`,
		`activity_relay_queue_backlog 2`,
		`activity_relay_queue_reservations 4`,
		`activity_relay_receivers{kind="unique"} 2`,
		`activity_relay_publishers 3`,
		`activity_relay_receiver_health{state="failing"} 1`,
		`activity_relay_operational_collection_success 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics omitted %q\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"PrivateType", "raw error text", "private-origin", "directory.example"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("metrics exposed unbounded value %q", forbidden)
		}
	}
}

func TestClassifyDeliveryError(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "none"},
		{context.DeadlineExceeded, "timeout"},
		{&net.DNSError{Err: "no such host", Name: "example.invalid"}, "dns"},
		{errors.New("remote: 404 Not Found"), "http_4xx"},
		{errors.New("remote: 503 Service Unavailable"), "http_5xx"},
		{errors.New("tls: failed to verify certificate"), "tls"},
		{errors.New("create delivery request: missing protocol scheme"), "invalid_url"},
		{errors.New("opaque failure"), "other"},
	}
	for _, test := range tests {
		if got := ClassifyDeliveryError(test.err); got != test.want {
			t.Errorf("ClassifyDeliveryError(%v) = %q; want %q", test.err, got, test.want)
		}
	}
}

func TestOperationalCollectorBoundsExistingLedgerLabels(t *testing.T) {
	client := operationalTestRedis(t)
	ctx := context.Background()
	if err := client.HSet(ctx, OperationalMetricsKey, map[string]interface{}{
		"activities_total|invented|PrivateType|raw.example/path": 2,
		"activities_total|other|other|other":                     3,
		"http_signature_verifications_total|private|raw|secret":  4,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	collector := NewOperationalCollector(client, nil)
	registry := prometheus.NewRegistry()
	if err := registry.Register(collector); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(
		recorder,
		httptest.NewRequest("GET", "/metrics", nil),
	)
	body := recorder.Body.String()
	if !strings.Contains(body, `activity_relay_activities_total{reason="other",result="other",type="other"} 5`) {
		t.Fatalf("bounded ledger values were not combined\n%s", body)
	}
	if !strings.Contains(
		body,
		`activity_relay_http_signature_verifications_total{profile="other",reason="other",result="other"} 4`,
	) {
		t.Fatalf("bounded signature ledger values were not exported\n%s", body)
	}
	for _, forbidden := range []string{
		"invented",
		"PrivateType",
		"raw.example",
		"private",
		"secret",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("collector exposed unbounded ledger label %q", forbidden)
		}
	}
}
