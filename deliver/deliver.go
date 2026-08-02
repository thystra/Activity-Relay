package deliver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/RichardKnop/machinery/v2"
	"github.com/RichardKnop/machinery/v2/log"
	"github.com/RichardKnop/machinery/v2/retry"
	"github.com/RichardKnop/machinery/v2/tasks"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/deliverymeta"
	"github.com/thystra/Activity-Relay/internal/deliverypolicy"
	"github.com/thystra/Activity-Relay/internal/httpsignature"
	"github.com/thystra/Activity-Relay/internal/observability"
	"github.com/thystra/Activity-Relay/models"
)

var (
	version      string
	GlobalConfig *models.RelayConfig

	// RelayActor : Relay's Actor
	RelayActor models.Actor

	HttpClient            *http.Client
	MachineryServer       *machinery.Server
	RedisClient           *redis.Client
	OperationalMetrics    *observability.Recorder
	OutboundRequestSigner *httpsignature.ConfiguredSigner
)

func recordDeliveryMetric(result, errorClass string) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordDelivery(context.Background(), result, errorClass); err != nil {
		logrus.WithError(err).Debug("Unable to record delivery metric")
	}
}

func recordWorkerRedisFailure(operation string) {
	if OperationalMetrics == nil {
		return
	}
	if err := OperationalMetrics.RecordRedisFailure(context.Background(), "worker", operation); err != nil {
		logrus.WithError(err).Debug("Unable to record Redis failure metric")
	}
}

type deliveryAttempt struct {
	TaskUUID         string
	Attempt          int
	MaxAttempts      int
	RetriesRemaining int
	NextRetrySeconds int
	Terminal         bool
}

func currentDeliveryAttempt(ctx context.Context) deliveryAttempt {
	signature := tasks.SignatureFromContext(ctx)
	if signature == nil {
		return deliveryAttempt{
			TaskUUID:         "direct",
			Attempt:          1,
			MaxAttempts:      1,
			RetriesRemaining: 0,
			Terminal:         true,
		}
	}

	retriesRemaining := signature.RetryCount
	attempt := deliverypolicy.MaxAttempts - retriesRemaining
	if attempt < 1 {
		attempt = 1
	}
	if attempt > deliverypolicy.MaxAttempts {
		attempt = deliverypolicy.MaxAttempts
	}

	nextRetrySeconds := 0
	if retriesRemaining > 0 {
		nextRetrySeconds = retry.FibonacciNext(signature.RetryTimeout)
	}

	return deliveryAttempt{
		TaskUUID:         signature.UUID,
		Attempt:          attempt,
		MaxAttempts:      deliverypolicy.MaxAttempts,
		RetriesRemaining: retriesRemaining,
		NextRetrySeconds: nextRetrySeconds,
		Terminal:         retriesRemaining <= 0,
	}
}

func boundedDeliveryLogValue(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func finishRelayTarget(activityID string) (int64, error) {
	const finishScript = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
local remaining = redis.call('HINCRBY', KEYS[1], 'remain_count', -1)
if remaining < 1 then
  redis.call('DEL', KEYS[1])
end
return remaining`

	remaining, err := RedisClient.Eval(
		context.Background(),
		finishScript,
		[]string{"relay:activity:" + activityID},
	).Int64()
	if err != nil {
		recordWorkerRedisFailure("activity_cleanup")
	}
	return remaining, err
}

func recordReceiverAttempt(receiverDomain string, succeeded bool) {
	if receiverDomain == "" {
		return
	}

	healthErr := models.RecordReceiverDelivery(
		context.Background(),
		RedisClient,
		receiverDomain,
		succeeded,
		time.Now().UTC(),
	)
	if healthErr != nil {
		recordWorkerRedisFailure("receiver_health")
		logrus.WithError(healthErr).
			WithField("receiver", receiverDomain).
			Warn("Unable to record receiver delivery health")
	}
}

func storeTransientDeliveryError(receiverDomain string, deliveryErr error) {
	if receiverDomain == "" || deliveryErr == nil {
		return
	}

	const pushErrorLogScript = `
local changed = redis.call('HSETNX', KEYS[1], 'last_error', ARGV[1])
if changed == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return changed`

	if statisticsErr := RedisClient.Eval(
		context.Background(),
		pushErrorLogScript,
		[]string{"relay:statistics:" + receiverDomain},
		boundedDeliveryLogValue(deliveryErr.Error(), 4096),
		60,
	).Err(); statisticsErr != nil {
		recordWorkerRedisFailure("statistics")
		logrus.WithError(statisticsErr).
			Debug("Unable to store transient delivery error")
	}
}

func relayActivityV2(ctx context.Context, args ...string) error {
	if len(args) < 2 {
		return fmt.Errorf("relay-v2 requires inbox URL and activity storage ID")
	}

	inboxURL := args[0]
	activityStorageID := args[1]
	attempt := currentDeliveryAttempt(ctx)
	receiverDomain, _ := models.ReceiverDomainFromInboxURL(inboxURL)

	fields := logrus.Fields{
		"task_uuid":           attempt.TaskUUID,
		"activity_storage_id": activityStorageID,
		"receiver":            boundedDeliveryLogValue(inboxURL, 2048),
		"receiver_domain":     receiverDomain,
		"attempt":             attempt.Attempt,
		"max_attempts":        attempt.MaxAttempts,
		"retries_remaining":   attempt.RetriesRemaining,
	}

	body, err := RedisClient.HGet(
		context.Background(),
		"relay:activity:"+activityStorageID,
		"body",
	).Result()
	if err != nil {
		errorClass := "activity_expired"
		if !errors.Is(err, redis.Nil) {
			errorClass = "other"
			recordWorkerRedisFailure("activity_load")
		}

		fields["error_class"] = errorClass
		fields["error"] = boundedDeliveryLogValue(err.Error(), 1024)
		recordDeliveryMetric("skipped", errorClass)

		if !attempt.Terminal {
			fields["next_retry_seconds"] = attempt.NextRetrySeconds
			fields["next_retry_at"] = time.Now().UTC().
				Add(time.Duration(attempt.NextRetrySeconds) * time.Second).
				Format(time.RFC3339Nano)
			logrus.WithFields(fields).
				Warn("Relay activity body unavailable; retry scheduled")
			return fmt.Errorf("load relay activity body: %w", err)
		}

		if remaining, cleanupErr := finishRelayTarget(activityStorageID); cleanupErr != nil {
			logrus.WithError(cleanupErr).
				WithFields(fields).
				Warn("Unable to finalize expired relay target")
		} else {
			fields["remaining_targets"] = remaining
		}

		logrus.WithFields(fields).
			Error("Relay activity body unavailable after retries exhausted")
		return fmt.Errorf("load relay activity body: %w", err)
	}

	metadata := deliverymeta.FromBody([]byte(body))
	fields["activity_id"] = metadata.ActivityID
	fields["activity_type"] = metadata.ActivityType
	fields["object_id"] = metadata.ObjectID
	fields["origin_actor"] = metadata.ActorID
	fields["origin_domain"] = metadata.OriginDomain
	fields["body_sha256"] = metadata.BodySHA256

	logrus.WithFields(fields).Debug("Relay delivery attempt started")
	started := time.Now()

	response, deliveryErr := sendActivityWithResponse(
		inboxURL,
		RelayActor.PublicKey.ID,
		[]byte(body),
		GlobalConfig.ActorKey(),
	)

	elapsed := time.Since(started)
	fields["elapsed_ms"] = elapsed.Milliseconds()
	fields["http_status"] = response.StatusCode
	fields["response_body"] = boundedDeliveryLogValue(response.Body, 4096)
	fields["response_truncated"] = response.BodyTruncated

	if deliveryErr == nil {
		recordDeliveryMetric("success", "none")
		recordReceiverAttempt(receiverDomain, true)

		remaining, cleanupErr := finishRelayTarget(activityStorageID)
		if cleanupErr != nil {
			logrus.WithError(cleanupErr).
				WithFields(fields).
				Warn("Delivery succeeded but target cleanup failed")
		} else {
			fields["remaining_targets"] = remaining
		}

		logrus.WithFields(fields).Debug("Relay delivery succeeded")
		return nil
	}

	errorClass := observability.ClassifyDeliveryError(deliveryErr)
	fields["error_class"] = errorClass
	fields["error"] = boundedDeliveryLogValue(deliveryErr.Error(), 4096)

	recordDeliveryMetric("failure", errorClass)
	recordReceiverAttempt(receiverDomain, false)
	storeTransientDeliveryError(receiverDomain, deliveryErr)

	if !attempt.Terminal {
		fields["next_retry_seconds"] = attempt.NextRetrySeconds
		fields["next_retry_at"] = time.Now().UTC().
			Add(time.Duration(attempt.NextRetrySeconds) * time.Second).
			Format(time.RFC3339Nano)

		logrus.WithFields(fields).
			Warn("Relay delivery failed; retry scheduled")
		return deliveryErr
	}

	remaining, cleanupErr := finishRelayTarget(activityStorageID)
	if cleanupErr != nil {
		logrus.WithError(cleanupErr).
			WithFields(fields).
			Warn("Unable to finalize exhausted relay target")
	} else {
		fields["remaining_targets"] = remaining
	}

	logrus.WithFields(fields).
		Error("Relay delivery failed after retries exhausted")
	return deliveryErr
}

func registerActivity(args ...string) error {
	inboxURL := args[0]
	body := args[1]
	err := sendActivity(inboxURL, RelayActor.PublicKey.ID, []byte(body), GlobalConfig.ActorKey())
	return err
}

func Entrypoint(g *models.RelayConfig, v string) error {
	var err error

	version = v
	GlobalConfig = g

	err = initialize(GlobalConfig)
	if err != nil {
		return err
	}

	err = MachineryServer.RegisterTask("register", registerActivity)
	if err != nil {
		return err
	}
	err = MachineryServer.RegisterTask("relay-v2", relayActivityV2)
	if err != nil {
		return err
	}

	workerID := uuid.New()
	worker := MachineryServer.NewWorker(workerID.String(), GlobalConfig.JobConcurrency())
	err = worker.Launch()
	if err != nil {
		logrus.Error(err)
	}

	return nil
}

func initialize(globalConfig *models.RelayConfig) error {
	var err error

	RedisClient = globalConfig.RedisClient()
	OperationalMetrics = nil
	if globalConfig.ObservabilityBind() != "" {
		OperationalMetrics = observability.NewRecorder(RedisClient)
	}

	MachineryServer, err = models.NewMachineryServer(globalConfig)
	if err != nil {
		return err
	}
	HttpClient = &http.Client{Timeout: time.Duration(5) * time.Second}

	RelayActor = models.NewActivityPubActorFromRelayConfig(globalConfig)
	OutboundRequestSigner, err = httpsignature.NewConfiguredSigner(
		RelayActor.PublicKey.ID,
		globalConfig.ActorKey(),
		globalConfig.OutboundSignatureProfile(),
	)
	if err != nil {
		return err
	}
	newNullLogger := NewNullLogger()
	log.DEBUG = newNullLogger

	return nil
}
