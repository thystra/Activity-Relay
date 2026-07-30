package observability

import (
	"context"
	"errors"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

const OperationalMetricsKey = "relay:metrics:operational"
const operationalCollectionTimeout = 500 * time.Millisecond
const operationalWriteTimeout = 250 * time.Millisecond
const metricFieldSeparator = "|"

var deliveryHTTPStatusPattern = regexp.MustCompile(`(?:^|[: ])([45][0-9]{2})(?: |$)`)

// Recorder stores bounded relay-wide counters in Redis for collection by the API process.
type Recorder struct {
	client *redis.Client
}

// RuntimeSnapshot contains current aggregate state without private instance identifiers.
type RuntimeSnapshot struct {
	Subscribers         int
	Followers           int
	UniqueReceivers     int
	Publishers          int
	HealthyReceivers    int
	FailingReceivers    int
	UnobservedReceivers int
}

// RuntimeSnapshotFunc returns aggregate relay state for one metrics scrape.
type RuntimeSnapshotFunc func(context.Context) (RuntimeSnapshot, error)

// NewRecorder creates a Redis-backed operational metrics recorder.
func NewRecorder(client *redis.Client) *Recorder {
	if client == nil {
		return nil
	}
	return &Recorder{client: client}
}

func (recorder *Recorder) increment(ctx context.Context, metric string, delta int64, labels ...string) error {
	if recorder == nil || recorder.client == nil || delta < 1 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, operationalWriteTimeout)
		defer cancel()
	}
	field := strings.Join(append([]string{metric}, labels...), metricFieldSeparator)
	return recorder.client.HIncrBy(ctx, OperationalMetricsKey, field, delta).Err()
}

// RecordActivity records one accepted, rejected, or ignored inbox activity.
func (recorder *Recorder) RecordActivity(ctx context.Context, result, activityType, reason string) error {
	return recorder.increment(ctx, "activities_total", 1,
		normalizeActivityResult(result), normalizeActivityType(activityType), normalizeActivityReason(reason))
}

// RecordQueueAdmission records one queue-admission decision.
func (recorder *Recorder) RecordQueueAdmission(ctx context.Context, kind, result, reason string) error {
	return recorder.increment(ctx, "queue_admissions_total", 1,
		normalizeQueueKind(kind), normalizeQueueResult(result), normalizeQueueReason(reason))
}

// RecordFanoutTargets adds a bounded number of fan-out targets to one outcome.
func (recorder *Recorder) RecordFanoutTargets(ctx context.Context, result string, count int) error {
	if count < 1 {
		return nil
	}
	return recorder.increment(ctx, "fanout_targets_total", int64(count), normalizeFanoutResult(result))
}

// RecordDelivery records one relay-v2 delivery result.
func (recorder *Recorder) RecordDelivery(ctx context.Context, result, errorClass string) error {
	return recorder.increment(ctx, "delivery_attempts_total", 1,
		normalizeDeliveryResult(result), normalizeDeliveryErrorClass(errorClass))
}

// RecordRedisFailure records one bounded Redis operation failure when Redis can accept the ledger write.
func (recorder *Recorder) RecordRedisFailure(ctx context.Context, component, operation string) error {
	return recorder.increment(ctx, "redis_operation_failures_total", 1,
		normalizeRedisComponent(component), normalizeRedisOperation(operation))
}

// ClassifyDeliveryError maps a delivery error to a bounded Prometheus label.
func ClassifyDeliveryError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "timeout"
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return "dns"
	}
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		return ClassifyDeliveryError(urlError.Err)
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(text, "no such host") || strings.Contains(text, "dns") {
		return "dns"
	}
	if strings.Contains(text, "tls") || strings.Contains(text, "x509") || strings.Contains(text, "certificate") {
		return "tls"
	}
	if strings.Contains(text, "unsupported protocol scheme") ||
		strings.Contains(text, "missing protocol scheme") ||
		strings.Contains(text, "invalid url") ||
		strings.Contains(text, "create delivery request") {
		return "invalid_url"
	}
	if match := deliveryHTTPStatusPattern.FindStringSubmatch(text); len(match) == 2 {
		if strings.HasPrefix(match[1], "4") {
			return "http_4xx"
		}
		return "http_5xx"
	}
	return "other"
}

func normalizeActivityResult(value string) string {
	return boundedValue(value, "other", "accepted", "rejected", "ignored")
}
func normalizeActivityType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "create":
		return "Create"
	case "update":
		return "Update"
	case "delete":
		return "Delete"
	case "move":
		return "Move"
	case "announce":
		return "Announce"
	case "follow":
		return "Follow"
	case "undo":
		return "Undo"
	case "accept":
		return "Accept"
	case "reject":
		return "Reject"
	default:
		return "other"
	}
}
func normalizeActivityReason(value string) string {
	return boundedValue(value, "other", "accepted", "invalid", "policy", "method", "internal", "unsupported")
}
func normalizeQueueKind(value string) string {
	return boundedValue(value, "other", "register", "relay")
}
func normalizeQueueResult(value string) string {
	return boundedValue(value, "other", "accepted", "rejected", "error", "skipped")
}
func normalizeQueueReason(value string) string {
	return boundedValue(value, "other", "accepted", "capacity", "fanout_limit", "redis", "store", "group", "broker", "no_targets")
}
func normalizeFanoutResult(value string) string {
	return boundedValue(value, "other", "queued", "rejected", "error")
}
func normalizeDeliveryResult(value string) string {
	return boundedValue(value, "other", "success", "failure", "skipped")
}
func normalizeDeliveryErrorClass(value string) string {
	return boundedValue(value, "other", "none", "timeout", "dns", "tls", "http_4xx", "http_5xx", "invalid_url", "activity_expired")
}
func normalizeRedisComponent(value string) string {
	return boundedValue(value, "other", "api", "worker", "collector")
}
func normalizeRedisOperation(value string) string {
	return boundedValue(value, "other", "queue_reserve", "queue_release", "activity_store", "activity_load", "receiver_health", "statistics", "activity_cleanup", "ledger", "queue_snapshot", "state_snapshot")
}
func boundedValue(value, fallback string, allowed ...string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range allowed {
		if value == candidate {
			return candidate
		}
	}
	return fallback
}

// OperationalCollector exports relay-wide Redis counters and current aggregate gauges.
type OperationalCollector struct {
	client       *redis.Client
	snapshot     RuntimeSnapshotFunc
	activities   *prometheus.Desc
	queue        *prometheus.Desc
	fanout       *prometheus.Desc
	delivery     *prometheus.Desc
	redisFailure *prometheus.Desc
	queueBacklog *prometheus.Desc
	reservations *prometheus.Desc
	receivers    *prometheus.Desc
	publishers   *prometheus.Desc
	health       *prometheus.Desc
	collectionOK *prometheus.Desc
}

// NewOperationalCollector creates a bounded scrape-time collector.
func NewOperationalCollector(client *redis.Client, snapshot RuntimeSnapshotFunc) *OperationalCollector {
	if client == nil {
		return nil
	}
	return &OperationalCollector{
		client:       client,
		snapshot:     snapshot,
		activities:   prometheus.NewDesc("activity_relay_activities_total", "Accepted, rejected, and ignored inbox activities.", []string{"result", "type", "reason"}, nil),
		queue:        prometheus.NewDesc("activity_relay_queue_admissions_total", "Queue admission decisions.", []string{"kind", "result", "reason"}, nil),
		fanout:       prometheus.NewDesc("activity_relay_fanout_targets_total", "Fan-out targets by enqueue outcome.", []string{"result"}, nil),
		delivery:     prometheus.NewDesc("activity_relay_delivery_attempts_total", "Relay fan-out delivery results.", []string{"result", "error_class"}, nil),
		redisFailure: prometheus.NewDesc("activity_relay_redis_operation_failures_total", "Redis operation failures recorded while Redis remained writable.", []string{"component", "operation"}, nil),
		queueBacklog: prometheus.NewDesc("activity_relay_queue_backlog", "Current Machinery broker queue length.", nil, nil),
		reservations: prometheus.NewDesc("activity_relay_queue_reservations", "Current temporary queue-capacity reservations.", nil, nil),
		receivers:    prometheus.NewDesc("activity_relay_receivers", "Current receiver counts by bounded kind.", []string{"kind"}, nil),
		publishers:   prometheus.NewDesc("activity_relay_publishers", "Current observed publisher count.", nil, nil),
		health:       prometheus.NewDesc("activity_relay_receiver_health", "Current receivers by aggregate delivery-health state.", []string{"state"}, nil),
		collectionOK: prometheus.NewDesc("activity_relay_operational_collection_success", "Whether the latest operational metrics collection completed successfully.", nil, nil),
	}
}

func (collector *OperationalCollector) Describe(channel chan<- *prometheus.Desc) {
	channel <- collector.activities
	channel <- collector.queue
	channel <- collector.fanout
	channel <- collector.delivery
	channel <- collector.redisFailure
	channel <- collector.queueBacklog
	channel <- collector.reservations
	channel <- collector.receivers
	channel <- collector.publishers
	channel <- collector.health
	channel <- collector.collectionOK
}

func (collector *OperationalCollector) Collect(channel chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), operationalCollectionTimeout)
	defer cancel()
	collectionSuccess := 1.0

	pipe := collector.client.Pipeline()
	ledgerCommand := pipe.HGetAll(ctx, OperationalMetricsKey)
	queueCommand := pipe.LLen(ctx, "relay")
	reservationCommand := pipe.Get(ctx, "relay:queue:reservations")
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		collectionSuccess = 0
	} else {
		collector.collectLedger(channel, ledgerCommand.Val())
		channel <- prometheus.MustNewConstMetric(collector.queueBacklog, prometheus.GaugeValue, float64(queueCommand.Val()))
		reservations := int64(0)
		if value, err := reservationCommand.Int64(); err == nil {
			reservations = value
		} else if !errors.Is(err, redis.Nil) {
			collectionSuccess = 0
		}
		channel <- prometheus.MustNewConstMetric(collector.reservations, prometheus.GaugeValue, float64(reservations))
	}

	if collector.snapshot != nil {
		snapshot, err := collector.snapshot(ctx)
		if err != nil {
			collectionSuccess = 0
		} else {
			channel <- prometheus.MustNewConstMetric(collector.receivers, prometheus.GaugeValue, float64(snapshot.Subscribers), "subscription")
			channel <- prometheus.MustNewConstMetric(collector.receivers, prometheus.GaugeValue, float64(snapshot.Followers), "follower")
			channel <- prometheus.MustNewConstMetric(collector.receivers, prometheus.GaugeValue, float64(snapshot.UniqueReceivers), "unique")
			channel <- prometheus.MustNewConstMetric(collector.publishers, prometheus.GaugeValue, float64(snapshot.Publishers))
			channel <- prometheus.MustNewConstMetric(collector.health, prometheus.GaugeValue, float64(snapshot.HealthyReceivers), "healthy")
			channel <- prometheus.MustNewConstMetric(collector.health, prometheus.GaugeValue, float64(snapshot.FailingReceivers), "failing")
			channel <- prometheus.MustNewConstMetric(collector.health, prometheus.GaugeValue, float64(snapshot.UnobservedReceivers), "unobserved")
		}
	}
	channel <- prometheus.MustNewConstMetric(collector.collectionOK, prometheus.GaugeValue, collectionSuccess)
}

type ledgerSeries struct {
	desc   *prometheus.Desc
	labels []string
	value  float64
}

func (collector *OperationalCollector) collectLedger(channel chan<- prometheus.Metric, ledger map[string]string) {
	series := make(map[string]*ledgerSeries)
	add := func(desc *prometheus.Desc, metric string, value float64, labels ...string) {
		key := strings.Join(append([]string{metric}, labels...), metricFieldSeparator)
		if current, ok := series[key]; ok {
			current.value += value
			return
		}
		series[key] = &ledgerSeries{desc: desc, labels: labels, value: value}
	}

	for field, rawValue := range ledger {
		value, err := strconv.ParseFloat(rawValue, 64)
		if err != nil || value < 0 {
			continue
		}
		parts := strings.Split(field, metricFieldSeparator)
		switch {
		case len(parts) == 4 && parts[0] == "activities_total":
			add(collector.activities, parts[0], value,
				normalizeActivityResult(parts[1]), normalizeActivityType(parts[2]), normalizeActivityReason(parts[3]))
		case len(parts) == 4 && parts[0] == "queue_admissions_total":
			add(collector.queue, parts[0], value,
				normalizeQueueKind(parts[1]), normalizeQueueResult(parts[2]), normalizeQueueReason(parts[3]))
		case len(parts) == 2 && parts[0] == "fanout_targets_total":
			add(collector.fanout, parts[0], value, normalizeFanoutResult(parts[1]))
		case len(parts) == 3 && parts[0] == "delivery_attempts_total":
			add(collector.delivery, parts[0], value,
				normalizeDeliveryResult(parts[1]), normalizeDeliveryErrorClass(parts[2]))
		case len(parts) == 3 && parts[0] == "redis_operation_failures_total":
			add(collector.redisFailure, parts[0], value,
				normalizeRedisComponent(parts[1]), normalizeRedisOperation(parts[2]))
		}
	}

	keys := make([]string, 0, len(series))
	for key := range series {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		current := series[key]
		channel <- prometheus.MustNewConstMetric(current.desc, prometheus.CounterValue, current.value, current.labels...)
	}
}
