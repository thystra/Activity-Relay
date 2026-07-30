package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/observability"
	"github.com/thystra/Activity-Relay/models"
)

func relayRuntimeSnapshot(ctx context.Context) (observability.RuntimeSnapshot, error) {
	snapshot := RelayState.Snapshot()
	uniqueReceivers := make(map[string]struct{}, len(snapshot.SubscribersAndFollowers))
	for _, receiver := range snapshot.SubscribersAndFollowers {
		domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(receiver.Domain), "."))
		if domain != "" {
			uniqueReceivers[domain] = struct{}{}
		}
	}
	domains := make([]string, 0, len(uniqueReceivers))
	for domain := range uniqueReceivers {
		domains = append(domains, domain)
	}
	health, err := models.LoadReceiverDeliveryHealth(ctx, RelayState.RedisClient, domains)
	if err != nil {
		return observability.RuntimeSnapshot{}, err
	}
	result := observability.RuntimeSnapshot{
		Subscribers:     len(snapshot.Subscribers),
		Followers:       len(snapshot.Followers),
		UniqueReceivers: len(uniqueReceivers),
		Publishers:      len(snapshot.Publishers),
	}
	for _, domain := range domains {
		entry := health[domain]
		switch {
		case entry.TotalSuccesses == 0 && entry.TotalFailures == 0:
			result.UnobservedReceivers++
		case entry.ConsecutiveFailures > 0:
			result.FailingReceivers++
		default:
			result.HealthyReceivers++
		}
	}
	return result, nil
}

func recordActivityMetric(result, activityType, reason string) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordActivity(context.Background(), result, activityType, reason); err != nil {
		logrus.WithError(err).Debug("Unable to record activity metric")
	}
}

func recordQueueAdmission(kind, result, reason string) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordQueueAdmission(context.Background(), kind, result, reason); err != nil {
		logrus.WithError(err).Debug("Unable to record queue admission metric")
	}
}

func recordFanoutTargets(result string, count int) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordFanoutTargets(context.Background(), result, count); err != nil {
		logrus.WithError(err).Debug("Unable to record fan-out metric")
	}
}

func recordRedisOperationFailure(component, operation string) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordRedisFailure(context.Background(), component, operation); err != nil {
		logrus.WithError(err).Debug("Unable to record Redis failure metric")
	}
}

type inboxStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *inboxStatusRecorder) WriteHeader(status int) {
	if recorder.status != 0 {
		return
	}
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *inboxStatusRecorder) Write(body []byte) (int, error) {
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return recorder.ResponseWriter.Write(body)
}

func (recorder *inboxStatusRecorder) Status() int {
	if recorder.status == 0 {
		return http.StatusOK
	}
	return recorder.status
}

func classifyInboxMetric(status int) (string, string) {
	switch {
	case status >= 200 && status < 300:
		return "accepted", "accepted"
	case status == http.StatusMethodNotAllowed:
		return "ignored", "method"
	case status == http.StatusBadRequest:
		return "rejected", "invalid"
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "rejected", "policy"
	case status >= 500:
		return "rejected", "internal"
	default:
		return "rejected", "unsupported"
	}
}
