package models

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const receiverHealthKeyPrefix = "relay:receiver-health:"

const recordReceiverDeliveryScript = `
if ARGV[2] == "1" then
  redis.call("HSET", KEYS[1], "last_success_at", ARGV[1], "consecutive_failures", 0)
  redis.call("HINCRBY", KEYS[1], "total_successes", 1)
else
  redis.call("HSET", KEYS[1], "last_failure_at", ARGV[1])
  redis.call("HINCRBY", KEYS[1], "consecutive_failures", 1)
  redis.call("HINCRBY", KEYS[1], "total_failures", 1)
end
return 1
`

// ReceiverDeliveryHealth stores durable, non-sensitive delivery observations.
type ReceiverDeliveryHealth struct {
	Domain              string
	LastSuccessAt       string
	LastFailureAt       string
	ConsecutiveFailures int64
	TotalSuccesses      int64
	TotalFailures       int64
}

// ReceiverDomainFromInboxURL returns the normalized authority used for health keys.
func ReceiverDomainFromInboxURL(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", fmt.Errorf("parse receiver inbox URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported receiver inbox URL scheme %q", parsed.Scheme)
	}
	host := normalizeStateDomain(parsed.Hostname())
	if host == "" {
		return "", errors.New("receiver inbox URL has no host")
	}
	if port := parsed.Port(); port != "" {
		return net.JoinHostPort(host, port), nil
	}
	return host, nil
}

// RecordReceiverDelivery atomically records one relay fan-out result.
func RecordReceiverDelivery(
	ctx context.Context,
	client *redis.Client,
	domain string,
	succeeded bool,
	occurredAt time.Time,
) error {
	if client == nil {
		return errors.New("receiver health Redis client is nil")
	}
	domain = normalizeStateDomain(domain)
	if domain == "" {
		return errors.New("receiver domain is empty")
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	successValue := "0"
	if succeeded {
		successValue = "1"
	}
	return client.Eval(
		ctx,
		recordReceiverDeliveryScript,
		[]string{receiverHealthKeyPrefix + domain},
		occurredAt.UTC().Format(time.RFC3339),
		successValue,
	).Err()
}

// LoadReceiverDeliveryHealth loads health for only the requested current domains.
func LoadReceiverDeliveryHealth(
	ctx context.Context,
	client *redis.Client,
	domains []string,
) (map[string]ReceiverDeliveryHealth, error) {
	if client == nil {
		return nil, errors.New("receiver health Redis client is nil")
	}
	unique := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = normalizeStateDomain(domain)
		if domain != "" {
			unique[domain] = struct{}{}
		}
	}
	normalized := make([]string, 0, len(unique))
	for domain := range unique {
		normalized = append(normalized, domain)
	}
	sort.Strings(normalized)

	result := make(map[string]ReceiverDeliveryHealth, len(normalized))
	if len(normalized) == 0 {
		return result, nil
	}
	keys := make([]string, len(normalized))
	for index, domain := range normalized {
		keys[index] = receiverHealthKeyPrefix + domain
	}
	values, err := loadHashes(
		ctx,
		client,
		keys,
		"last_success_at",
		"last_failure_at",
		"consecutive_failures",
		"total_successes",
		"total_failures",
	)
	if err != nil {
		return nil, err
	}
	for index, domain := range normalized {
		consecutiveFailures, err := receiverHealthCounter(values[index], 2)
		if err != nil {
			return nil, fmt.Errorf("load receiver %s consecutive failures: %w", domain, err)
		}
		totalSuccesses, err := receiverHealthCounter(values[index], 3)
		if err != nil {
			return nil, fmt.Errorf("load receiver %s total successes: %w", domain, err)
		}
		totalFailures, err := receiverHealthCounter(values[index], 4)
		if err != nil {
			return nil, fmt.Errorf("load receiver %s total failures: %w", domain, err)
		}
		result[domain] = ReceiverDeliveryHealth{
			Domain:              domain,
			LastSuccessAt:       stringValue(values[index], 0),
			LastFailureAt:       stringValue(values[index], 1),
			ConsecutiveFailures: consecutiveFailures,
			TotalSuccesses:      totalSuccesses,
			TotalFailures:       totalFailures,
		}
	}
	return result, nil
}

func receiverHealthCounter(values []interface{}, index int) (int64, error) {
	value := stringValue(values, index)
	if value == "" {
		return 0, nil
	}
	counter, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, err
	}
	if counter < 0 {
		return 0, errors.New("counter is negative")
	}
	return counter, nil
}
