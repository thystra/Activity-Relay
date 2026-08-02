package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
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
func recordHTTPSignatureVerification(profile, result, reason string) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordHTTPSignatureVerification(
		context.Background(),
		profile,
		result,
		reason,
	); err != nil {
		logrus.WithError(err).Debug(
			"Unable to record HTTP signature verification metric",
		)
	}
}

func classifyHTTPSignatureVerificationError(err error) string {
	if err == nil {
		return "accepted"
	}
	if errors.Is(err, relayhttpsig.ErrRFC9421Replay) {
		return "replay"
	}

	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "content-digest") ||
		strings.Contains(text, "digest header"):
		return "digest"
	case strings.Contains(text, "older than") ||
		strings.Contains(text, "future") ||
		strings.Contains(text, "expired") ||
		strings.Contains(text, "expires") ||
		strings.Contains(text, "created parameter"):
		return "time"
	case strings.Contains(text, "authority"):
		return "authority"
	case strings.Contains(text, "actor") ||
		strings.Contains(text, "owner"):
		return "actor"
	case strings.Contains(text, "resolve") ||
		strings.Contains(text, "public key") ||
		strings.Contains(text, "keyid"):
		return "key"
	case strings.Contains(text, "reserve rfc 9421 nonce") ||
		strings.Contains(text, "redis"):
		return "redis"
	case strings.Contains(text, "covered component") ||
		strings.Contains(text, "algorithm") ||
		strings.Contains(text, "nonce parameter"):
		return "policy"
	case strings.Contains(text, "parse rfc 9421") ||
		strings.Contains(text, "find activitypub") ||
		strings.Contains(text, "structured field"):
		return "parse"
	case strings.Contains(text, "verify rfc 9421 signature") ||
		strings.Contains(text, "verification error"):
		return "crypto"
	case strings.Contains(text, "invalid character") ||
		strings.Contains(text, "activity has no actor"):
		return "activity"
	default:
		return "other"
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
