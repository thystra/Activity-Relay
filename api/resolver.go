package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/RichardKnop/machinery/v2/tasks"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/internal/deliverypolicy"
	"github.com/thystra/Activity-Relay/models"
)

var followersPathPattern = regexp.MustCompile(`/followers$`)

const queueReservationKey = "relay:queue:reservations"

const storeRelayActivityScript = `
redis.call('HSET', KEYS[1], 'body', ARGV[1], 'remain_count', ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1`

func contains(entries interface{}, key string) bool {
	switch entry := entries.(type) {
	case string:
		return entry == key
	case []string:
		for i := 0; i < len(entry); i++ {
			if entry[i] == key {
				return true
			}
		}
		return false
	case []models.Subscriber:
		for i := 0; i < len(entry); i++ {
			if entry[i].Domain == key {
				return true
			}
		}
		return false
	case []models.Follower:
		for i := 0; i < len(entry); i++ {
			if entry[i].Domain == key {
				return true
			}
		}
		return false
	}
	return false
}

func reserveQueueCapacity(additional int) bool {
	accepted, _ := reserveQueueCapacityWithReason(additional)
	return accepted
}

func reserveQueueCapacityWithReason(additional int) (bool, string) {
	if additional < 1 {
		return true, "accepted"
	}
	const reserveScript = `
local queued = redis.call('LLEN', KEYS[1])
local reserved = tonumber(redis.call('GET', KEYS[2]) or '0')
local additional = tonumber(ARGV[1])
local maximum = tonumber(ARGV[2])
if queued + reserved + additional > maximum then
  return 0
end
redis.call('INCRBY', KEYS[2], additional)
redis.call('EXPIRE', KEYS[2], 60)
return 1`
	reserved, err := RelayState.RedisClient.Eval(
		context.Background(), reserveScript, []string{"relay", queueReservationKey},
		additional, GlobalConfig.MaxQueueJobs(),
	).Int()
	if err != nil {
		logrus.Error("Unable to reserve relay queue capacity: ", err)
		recordRedisOperationFailure("api", "queue_reserve")
		return false, "redis"
	}
	if reserved != 1 {
		logrus.Warn("Skipped relay work: queue would exceed MAX_QUEUE_JOBS")
		return false, "capacity"
	}
	return true, "accepted"
}

func releaseQueueCapacity(additional int) {
	if additional < 1 {
		return
	}
	const releaseScript = `
local remaining = redis.call('DECRBY', KEYS[1], ARGV[1])
if remaining <= 0 then
  redis.call('DEL', KEYS[1])
end
return remaining`
	if err := RelayState.RedisClient.Eval(context.Background(), releaseScript, []string{queueReservationKey}, additional).Err(); err != nil {
		logrus.Error("Unable to release relay queue capacity: ", err)
		recordRedisOperationFailure("api", "queue_release")
	}
}
func enqueueRegisterActivity(inboxURL string, body []byte) {
	accepted, reason := reserveQueueCapacityWithReason(1)
	if !accepted {
		recordQueueAdmission("register", "rejected", reason)
		return
	}
	defer releaseQueueCapacity(1)
	job := &tasks.Signature{
		Name:       "register",
		RetryCount: 2,
		Args: []tasks.Arg{
			{
				Name:  "inboxURL",
				Type:  "string",
				Value: inboxURL,
			},
			{
				Name:  "body",
				Type:  "string",
				Value: string(body),
			},
		},
	}
	if _, err := MachineryServer.SendTask(job); err != nil {
		logrus.Error(err)
		recordQueueAdmission("register", "error", "broker")
		return
	}
	recordQueueAdmission("register", "accepted", "accepted")
}
func relayTask(inboxURL string, activityID string) *tasks.Signature {
	return &tasks.Signature{
		Name:         "relay-v2",
		RetryCount:   deliverypolicy.RetryCount,
		RetryTimeout: deliverypolicy.InitialRetryTimeoutSeconds,
		Args: []tasks.Arg{
			{
				Name:  "inboxURL",
				Type:  "string",
				Value: inboxURL,
			},
			{
				Name:  "activityID",
				Type:  "string",
				Value: activityID,
			},
		},
	}
}

func storeRelayActivity(activityID string, body []byte, targetCount int) error {
	stored, err := RelayState.RedisClient.Eval(
		context.Background(),
		storeRelayActivityScript,
		[]string{"relay:activity:" + activityID},
		body,
		targetCount,
		deliverypolicy.ActivityRetentionSeconds,
	).Int()
	if err != nil {
		recordRedisOperationFailure("api", "activity_store")
		return err
	}
	if stored != 1 {
		return fmt.Errorf("unexpected relay activity storage result: %d", stored)
	}
	return nil
}

func normalizedStoredDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	parsed, err := url.Parse("//" + domain)
	if err == nil && parsed.Hostname() != "" {
		return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	}
	return strings.ToLower(strings.TrimSuffix(domain, "."))
}

func enqueueActivityExcept(
	subscriptions []models.Subscriber,
	body []byte,
	excludedDomains ...string,
) bool {
	if len(subscriptions) > GlobalConfig.MaxFanoutTargets() {
		logrus.Warn("Skipped relay activity: fan-out exceeds MAX_FANOUT_TARGETS")
		recordQueueAdmission("relay", "rejected", "fanout_limit")
		recordFanoutTargets("rejected", len(subscriptions))
		return false
	}

	excluded := make(map[string]struct{}, len(excludedDomains))
	for _, domain := range excludedDomains {
		normalized := normalizedStoredDomain(domain)
		if normalized != "" {
			excluded[normalized] = struct{}{}
		}
	}

	targets := make([]models.Subscriber, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if _, skip := excluded[normalizedStoredDomain(subscription.Domain)]; !skip {
			targets = append(targets, subscription)
		}
	}
	if len(targets) < 1 {
		recordQueueAdmission("relay", "skipped", "no_targets")
		return true
	}
	accepted, reason := reserveQueueCapacityWithReason(len(targets))
	if !accepted {
		recordQueueAdmission("relay", "rejected", reason)
		recordFanoutTargets("rejected", len(targets))
		return false
	}
	defer releaseQueueCapacity(len(targets))
	activityID := uuid.NewString()
	if err := storeRelayActivity(activityID, body, len(targets)); err != nil {
		logrus.Error("Unable to store relay activity: ", err)
		recordQueueAdmission("relay", "error", "store")
		recordFanoutTargets("error", len(targets))
		return false
	}
	signatures := make([]*tasks.Signature, 0, len(targets))
	for _, target := range targets {
		signatures = append(signatures, relayTask(target.InboxURL, activityID))
	}
	group, err := tasks.NewGroup(signatures...)
	if err != nil {
		logrus.Error("Unable to create relay task group: ", err)
		recordQueueAdmission("relay", "error", "group")
		recordFanoutTargets("error", len(targets))
		return false
	}
	concurrency := len(signatures)
	if concurrency > 16 {
		concurrency = 16
	}
	if _, err := MachineryServer.SendGroup(group, concurrency); err != nil {
		logrus.Error("Unable to enqueue relay task group: ", err)
		recordQueueAdmission("relay", "error", "broker")
		recordFanoutTargets("error", len(targets))
		return false
	}
	recordQueueAdmission("relay", "accepted", "accepted")
	recordFanoutTargets("queued", len(targets))
	return true
}

func enqueueActivity(
	subscriptions []models.Subscriber,
	sourceDomain string,
	body []byte,
) {
	_ = enqueueActivityExcept(subscriptions, body, sourceDomain)
}

func enqueueActivityForAll(sourceDomain string, body []byte) {
	enqueueActivity(
		RelayState.Snapshot().SubscribersAndFollowers,
		sourceDomain,
		body,
	)
}

func enqueueActivityForAllExcept(
	body []byte,
	excludedDomains ...string,
) bool {
	return enqueueActivityExcept(
		RelayState.Snapshot().SubscribersAndFollowers,
		body,
		excludedDomains...,
	)
}

func enqueueActivityForSubscriber(sourceDomain string, body []byte) {
	enqueueActivity(RelayState.Snapshot().Subscribers, sourceDomain, body)
}

func enqueueActivityForFollower(sourceDomain string, body []byte) {
	snapshot := RelayState.Snapshot()
	subscriptions := make([]models.Subscriber, 0, len(snapshot.Followers))
	for _, follower := range snapshot.Followers {
		subscriptions = append(subscriptions, models.Subscriber{
			Domain: follower.Domain, InboxURL: follower.InboxURL,
			ActivityID: follower.ActivityID, ActorID: follower.ActorID,
		})
	}
	enqueueActivity(subscriptions, sourceDomain, body)
}

func normalizedActorDomain(actorID *url.URL) string {
	if actorID == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(actorID.Hostname(), "."))
}

func isActorLimited(actorID *url.URL) bool {
	return RelayState.IsLimited(normalizedActorDomain(actorID))
}

func isActorBlocked(actorID *url.URL) bool {
	return RelayState.IsBlocked(normalizedActorDomain(actorID))
}

func isActorSubscribed(actorID *url.URL) bool {
	return RelayState.IsSubscriber(normalizedActorDomain(actorID))
}

func isActorFollowers(actorID *url.URL) bool {
	return RelayState.IsFollower(normalizedActorDomain(actorID))
}

func isActorSubscribersOrFollowers(actorID *url.URL) bool {
	return RelayState.IsSubscriberOrFollower(normalizedActorDomain(actorID))
}

func isActorAbleToBeFollower(actor *models.Actor) bool {
	if actor == nil {
		return false
	}
	actorID, err := url.Parse(actor.ID)
	if err != nil || normalizedActorDomain(actorID) == "" {
		return false
	}

	// Modern server software commonly publishes an Application or Service
	// actor at /actor or another implementation-defined path.
	switch actor.Type {
	case "Application", "Service":
		return true
	}

	// Preserve compatibility with older LitePub and Friendica actors that may
	// omit or mislabel their server actor type.
	switch strings.TrimSuffix(actorID.Path, "/") {
	case "/relay", "/friendica":
		return true
	default:
		return false
	}
}

func isActorAbleToRelay(actor *models.Actor) bool {
	domain, err := url.Parse(actor.ID)
	if err != nil || normalizedActorDomain(domain) == "" {
		return false
	}
	if isActorBlocked(domain) || isActorLimited(domain) {
		return false
	}
	if RelayState.PersonOnly() && actor.Type != "Person" {
		return false
	}
	return true
}

func isToMyFollower(entries []string) bool {
	snapshot := RelayState.Snapshot()
	for _, entry := range entries {
		if followersPathPattern.MatchString(entry) {
			for _, follower := range snapshot.Followers {
				if follower.ActorID+"/followers" == entry {
					return true
				}
			}
		}
	}
	return false
}

func executeFollowing(activity *models.Activity, actor *models.Actor) error {
	actorID, _ := url.Parse(actor.ID)
	if isActorBlocked(actorID) {
		return errors.New(actorID.Host + " is blocked")
	}
	switch {
	case contains(activity.Object, "https://www.w3.org/ns/activitystreams#Public"):
		if RelayState.ManualApprovalRequired() {
			RelayState.RedisClient.HMSet(context.TODO(), "relay:pending:"+actorID.Host, map[string]interface{}{
				"inbox_url":   actor.Endpoints.SharedInbox,
				"activity_id": activity.ID,
				"type":        "Follow",
				"actor":       actor.ID,
				"object":      activity.Object.(string),
			})
			logrus.Info("Pending Follow Request : ", activity.Actor)
		} else {
			resp := activity.GenerateReply(RelayActor, activity, "Accept")
			jsonData, _ := json.Marshal(&resp)
			go enqueueRegisterActivity(actor.Inbox, jsonData)
			RelayState.AddSubscriber(models.Subscriber{
				Domain:     actorID.Host,
				InboxURL:   actor.Endpoints.SharedInbox,
				ActivityID: activity.ID,
				ActorID:    actor.ID,
			})
			logrus.Info("Accepted Follow Request : ", activity.Actor)
		}
	case contains(activity.Object, RelayActor.ID):
		if isActorAbleToBeFollower(actor) {
			if RelayState.ManualApprovalRequired() {
				RelayState.RedisClient.HMSet(context.TODO(), "relay:pending:"+actorID.Host, map[string]interface{}{
					"inbox_url":   actor.Endpoints.SharedInbox,
					"activity_id": activity.ID,
					"type":        "Follow",
					"actor":       actor.ID,
					"object":      activity.Object.(string),
				})
				logrus.Info("Pending Follow Request : ", activity.Actor)
			} else {
				resp := activity.GenerateReply(RelayActor, activity, "Accept")
				jsonData, _ := json.Marshal(&resp)
				go enqueueRegisterActivity(actor.Inbox, jsonData)
				follower := models.Follower{
					Domain:         actorID.Host,
					InboxURL:       actor.Inbox,
					ActivityID:     activity.ID,
					ActorID:        actor.ID,
					MutuallyFollow: false,
				}
				RelayState.AddFollower(follower)
				logrus.Info("Accepted Follow Request : ", activity.Actor)

				executeMutuallyFollow(follower)
			}
			return nil
		}
		fallthrough
	default:
		err := errors.New("only Public or the relay actor is allowed to be followed")
		return err
	}
	return nil
}

func executeUnfollowing(activity *models.Activity, actor *models.Actor) error {
	actorID, _ := url.Parse(actor.ID)
	switch {
	case contains(activity.Object, "https://www.w3.org/ns/activitystreams#Public"):
		RelayState.DelSubscriber(actorID.Host)
		logrus.Info("Accepted Unfollow Request : ", activity.Actor)
		return nil
	case contains(activity.Object, RelayActor.ID):
		if isActorAbleToBeFollower(actor) {
			RelayState.DelFollower(actorID.Host)
			logrus.Info("Accepted Unfollow Request : ", activity.Actor)
			return nil
		}
		fallthrough
	default:
		err := errors.New("only Public or the relay actor is allowed to be unfollowed")
		return err
	}
}

func executeMutuallyFollow(follower models.Follower) error {
	actorID, _ := url.Parse(follower.ActorID)
	if !isActorLimited(actorID) {
		followRequest := models.NewActivityPubActivity(RelayActor, []string{follower.ActorID}, follower.ActorID, "Follow")
		jsonData, _ := json.Marshal(&followRequest)
		go enqueueRegisterActivity(follower.InboxURL, jsonData)
		logrus.Info("Sent MutuallyFollow Request : ", follower.ActorID)
	}
	return nil
}

func finalizeMutuallyFollow(activity *models.Activity, actor *models.Actor, activityType string) {
	actorID, _ := url.Parse(actor.ID)
	if contains(activity.Actor, RelayActor.ID) && contains(activity.Object, actor.ID) && isActorFollowers(actorID) {
		RelayState.UpdateFollowerStatus(actorID.Host, activityType == "Accept")
		logrus.Info("Confirmed MutuallyFollow "+activityType+"ed : ", actor.ID)
	}
}

func executeRejectRequest(activity *models.Activity, actor *models.Actor, err error) {
	reject := activity.GenerateReply(RelayActor, activity, "Reject")
	jsonData, _ := json.Marshal(&reject)
	go enqueueRegisterActivity(actor.Inbox, jsonData)
	logrus.Error("Rejected Follow, Unfollow Request : ", activity.Actor, " ", err.Error())
}

func publisherInbox(actor *models.Actor) string {
	if actor.Endpoints != nil && actor.Endpoints.SharedInbox != "" {
		return actor.Endpoints.SharedInbox
	}
	return actor.Inbox
}

func recordPublisherActivity(activity *models.Activity, actor *models.Actor) error {
	actorID, err := url.Parse(actor.ID)
	if err != nil || normalizedActorDomain(actorID) == "" {
		return errors.New("activity actor has an invalid ID")
	}
	if isActorBlocked(actorID) {
		return errors.New(normalizedActorDomain(actorID) + " is blocked")
	}
	if !isActorAbleToRelay(actor) {
		return nil
	}
	return RelayState.RecordPublisherActivity(models.Publisher{
		Domain:           normalizedActorDomain(actorID),
		ActorID:          actor.ID,
		InboxURL:         publisherInbox(actor),
		LastActivityID:   activity.ID,
		LastActivityType: activity.Type,
	})
}

func executeEmbeddedAnnounceActivity(activity *models.Activity, actor *models.Actor) error {
	actorID, err := url.Parse(actor.ID)
	if err != nil || normalizedActorDomain(actorID) == "" {
		return errors.New("activity actor has an invalid ID")
	}
	sourceDomain := normalizedActorDomain(actorID)
	if isActorBlocked(actorID) {
		return errors.New(sourceDomain + " is blocked")
	}
	if !isActorAbleToRelay(actor) {
		logrus.Debug("Skipped embedded Announce Activity : ", activity.Actor)
		return nil
	}

	objectID, err := activity.UnwrapInnerObjectId()
	if err != nil {
		return err
	}
	if err := recordPublisherActivity(activity, actor); err != nil {
		return err
	}

	announce := models.NewActivityPubActivity(
		RelayActor,
		[]string{RelayActor.Followers()},
		objectID,
		"Announce",
	)
	jsonData, err := json.Marshal(&announce)
	if err != nil {
		return err
	}
	go enqueueActivityForAll(sourceDomain, jsonData)
	logrus.WithFields(logrus.Fields{
		"actor":  activity.Actor,
		"object": objectID,
	}).Info("Accepted public embedded Announce for fan-out")
	return nil
}

func executeRelayActivity(activity *models.Activity, actor *models.Actor, body []byte) error {
	actorID, err := url.Parse(actor.ID)
	if err != nil || normalizedActorDomain(actorID) == "" {
		return errors.New("activity actor has an invalid ID")
	}
	if isActorBlocked(actorID) {
		return errors.New(normalizedActorDomain(actorID) + " is blocked")
	}
	if isActorAbleToRelay(actor) {
		if err := recordPublisherActivity(activity, actor); err != nil {
			return err
		}
		go enqueueActivityForSubscriber(normalizedActorDomain(actorID), body)

		var innnerObjectId, err = activity.UnwrapInnerObjectId()
		if err != nil {
			logrus.Debug("Accepted Relay Activity (Announce Failed) : ", activity.Actor)
		} else {
			announce := models.NewActivityPubActivity(RelayActor, []string{RelayActor.Followers()}, innnerObjectId, "Announce")
			jsonData, _ := json.Marshal(&announce)
			go enqueueActivityForFollower(normalizedActorDomain(actorID), jsonData)
			logrus.Debug("Accepted Relay Activity : ", activity.Actor)
		}
	} else {
		logrus.Debug("Skipped Relay Activity : ", activity.Actor)
	}
	return nil
}

func executeAnnounceActivity(
	activity *models.Activity,
	actor *models.Actor,
	sourceRelayDomain string,
) error {
	actorID, err := url.Parse(actor.ID)
	if err != nil || normalizedActorDomain(actorID) == "" {
		return errors.New("activity actor has an invalid ID")
	}
	originDomain := normalizedActorDomain(actorID)
	if isActorBlocked(actorID) {
		return errors.New(originDomain + " is blocked")
	}
	if !isActorAbleToRelay(actor) {
		logrus.Debug("Skipped Announce Activity : ", activity.Actor)
		return nil
	}

	reservation, accepted, err := reserveCanonicalRelayActivity(activity.ID)
	if err != nil {
		logrus.WithError(err).Error(
			"Unable to reserve canonical relay activity",
		)
		return nil
	}
	if !accepted {
		recordQueueAdmission("relay", "skipped", "canonical_duplicate")
		logrus.Debug("Skipped duplicate canonical Announce Activity : ", activity.ID)
		return nil
	}

	if err := recordPublisherActivity(activity, actor); err != nil {
		releaseCanonicalRelayActivity(reservation)
		return err
	}
	announce := models.NewActivityPubActivity(
		RelayActor,
		[]string{RelayActor.Followers()},
		activity.ID,
		"Announce",
	)
	jsonData, err := json.Marshal(&announce)
	if err != nil {
		releaseCanonicalRelayActivity(reservation)
		return err
	}
	go func() {
		if !enqueueActivityForAllExcept(
			jsonData,
			sourceRelayDomain,
			originDomain,
		) {
			releaseCanonicalRelayActivity(reservation)
		}
	}()
	logrus.Debug("Accepted Announce Activity : ", activity.Actor)
	return nil
}
