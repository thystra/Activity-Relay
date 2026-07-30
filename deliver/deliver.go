package deliver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/RichardKnop/machinery/v2"
	"github.com/RichardKnop/machinery/v2/log"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/observability"
	"github.com/thystra/Activity-Relay/models"
)

var (
	version      string
	GlobalConfig *models.RelayConfig

	// RelayActor : Relay's Actor
	RelayActor models.Actor

	HttpClient         *http.Client
	MachineryServer    *machinery.Server
	RedisClient        *redis.Client
	OperationalMetrics *observability.Recorder
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

func relayActivityV2(args ...string) error {
	inboxURL := args[0]
	activityID := args[1]
	body, err := RedisClient.HGet(context.TODO(), "relay:activity:"+activityID, "body").Result()
	if err != nil {
		errorClass := "activity_expired"
		if !errors.Is(err, redis.Nil) {
			errorClass = "other"
			recordWorkerRedisFailure("activity_load")
		}
		recordDeliveryMetric("skipped", errorClass)
		return errors.New("activity ttl expired")
	}

	err = sendActivity(inboxURL, RelayActor.PublicKey.ID, []byte(body), GlobalConfig.ActorKey())
	if err == nil {
		recordDeliveryMetric("success", "none")
	} else {
		recordDeliveryMetric("failure", observability.ClassifyDeliveryError(err))
	}
	receiverDomain, domainErr := models.ReceiverDomainFromInboxURL(inboxURL)
	if domainErr == nil {
		healthErr := models.RecordReceiverDelivery(
			context.Background(),
			RedisClient,
			receiverDomain,
			err == nil,
			time.Now().UTC(),
		)
		if healthErr != nil {
			recordWorkerRedisFailure("receiver_health")
			logrus.WithError(healthErr).WithField("receiver", receiverDomain).Warn("Unable to record receiver delivery health")
		}
	}
	if err != nil && domainErr == nil {
		pushErrorLogScript := "local change = redis.call('HSETNX', KEYS[1], 'last_error', ARGV[1]); if change == 1 then redis.call('EXPIRE', KEYS[1], ARGV[2]) end;"
		if statisticsErr := RedisClient.Eval(
			context.TODO(),
			pushErrorLogScript,
			[]string{"relay:statistics:" + receiverDomain},
			err.Error(),
			60,
		).Err(); statisticsErr != nil {
			recordWorkerRedisFailure("statistics")
			logrus.WithError(statisticsErr).Debug("Unable to store transient delivery error")
		}
	}
	reductionRemainCountScript := "local remain_count = redis.call('HINCRBY', KEYS[1], 'remain_count', -1); if remain_count < 1 then redis.call('DEL', KEYS[1]) end;"
	if cleanupErr := RedisClient.Eval(
		context.TODO(),
		reductionRemainCountScript,
		[]string{"relay:activity:" + activityID},
	).Err(); cleanupErr != nil {
		recordWorkerRedisFailure("activity_cleanup")
		logrus.WithError(cleanupErr).Debug("Unable to update relay activity remaining count")
	}
	return err
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
	newNullLogger := NewNullLogger()
	log.DEBUG = newNullLogger

	return nil
}
