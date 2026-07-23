package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/yukimochi/Activity-Relay/models"
	"github.com/yukimochi/machinery-v1/v1/tasks"
)

var followersPathPattern = regexp.MustCompile(`/followers$`)

const queueReservationKey = "relay:queue:reservations"

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
	if additional < 1 {
		return true
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
		return false
	}
	if reserved != 1 {
		logrus.Warn("Skipped relay work: queue would exceed MAX_QUEUE_JOBS")
		return false
	}
	return true
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
	}
}

func enqueueRegisterActivity(inboxURL string, body []byte) {
	if !reserveQueueCapacity(1) {
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
	_, err := MachineryServer.SendTask(job)
	if err != nil {
		logrus.Error(err)
	}
}

func relayTask(inboxURL string, activityID string) *tasks.Signature {
	return &tasks.Signature{
		Name:       "relay-v2",
		RetryCount: 0,
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

func enqueueActivity(subscriptions []models.Subscriber, sourceDomain string, body []byte) {
	if len(subscriptions) > GlobalConfig.MaxFanoutTargets() {
		logrus.Warn("Skipped relay activity: fan-out exceeds MAX_FANOUT_TARGETS")
		return
	}
	targets := make([]models.Subscriber, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if sourceDomain != subscription.Domain {
			targets = append(targets, subscription)
		}
	}
	if len(targets) < 1 {
		return
	}
	if !reserveQueueCapacity(len(targets)) {
		return
	}
	defer releaseQueueCapacity(len(targets))

	activityID := uuid.NewString()
	pushActivityScript := "redis.call('HSET',KEYS[1], 'body', ARGV[1], 'remain_count', ARGV[2]); redis.call('EXPIRE', KEYS[1], ARGV[3]);"
	if err := RelayState.RedisClient.Eval(context.Background(), pushActivityScript, []string{"relay:activity:" + activityID}, body, len(targets), 2*60).Err(); err != nil {
		logrus.Error("Unable to store relay activity: ", err)
		return
	}

	signatures := make([]*tasks.Signature, 0, len(targets))
	for _, target := range targets {
		signatures = append(signatures, relayTask(target.InboxURL, activityID))
	}
	group, err := tasks.NewGroup(signatures...)
	if err != nil {
		logrus.Error("Unable to create relay task group: ", err)
		return
	}
	concurrency := len(signatures)
	if concurrency > 16 {
		concurrency = 16
	}
	if _, err := MachineryServer.SendGroup(group, concurrency); err != nil {
		logrus.Error("Unable to enqueue relay task group: ", err)
	}
}

func enqueueActivityForAll(sourceDomain string, body []byte) {
	enqueueActivity(RelayState.Snapshot().SubscribersAndFollowers, sourceDomain, body)
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

func isActorLimited(actorID *url.URL) bool {
	return RelayState.IsLimited(actorID.Host)
}

func isActorBlocked(actorID *url.URL) bool {
	return RelayState.IsBlocked(actorID.Host)
}

func isActorSubscribed(actorID *url.URL) bool {
	return RelayState.IsSubscriber(actorID.Host)
}

func isActorFollowers(actorID *url.URL) bool {
	return RelayState.IsFollower(actorID.Host)
}

func isActorSubscribersOrFollowers(actorID *url.URL) bool {
	return RelayState.IsSubscriberOrFollower(actorID.Host)
}

func isActorAbleToBeFollower(actorID *url.URL) bool {
	switch strings.TrimSuffix(actorID.Path, "/") {
	case "/relay", "/friendica":
		return true
	default:
		return false
	}
}

func isActorAbleToRelay(actor *models.Actor) bool {
	domain, _ := url.Parse(actor.ID)
	if RelayState.IsLimited(domain.Host) {
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
		if isActorAbleToBeFollower(actorID) {
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
		err := errors.New("only https://www.w3.org/ns/activitystreams#Public is allowed to follow")
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
		if isActorAbleToBeFollower(actorID) {
			RelayState.DelFollower(actorID.Host)
			logrus.Info("Accepted Unfollow Request : ", activity.Actor)
			return nil
		}
		fallthrough
	default:
		err := errors.New("only https://www.w3.org/ns/activitystreams#Public is allowed to unfollow")
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

func executeRelayActivity(activity *models.Activity, actor *models.Actor, body []byte) error {
	actorID, _ := url.Parse(actor.ID)
	if !isActorSubscribed(actorID) {
		err := errors.New("to use the relay service, please follow in advance")
		return err
	}
	if isActorAbleToRelay(actor) {
		go enqueueActivityForSubscriber(actorID.Host, body)

		var innnerObjectId, err = activity.UnwrapInnerObjectId()
		if err != nil {
			logrus.Debug("Accepted Relay Activity (Announce Failed) : ", activity.Actor)
		} else {
			announce := models.NewActivityPubActivity(RelayActor, []string{RelayActor.Followers()}, innnerObjectId, "Announce")
			jsonData, _ := json.Marshal(&announce)
			go enqueueActivityForFollower(actorID.Host, jsonData)
			logrus.Debug("Accepted Relay Activity : ", activity.Actor)
		}
	} else {
		logrus.Debug("Skipped Relay Activity : ", activity.Actor)
	}
	return nil
}

func executeAnnounceActivity(activity *models.Activity, actor *models.Actor) error {
	actorID, _ := url.Parse(actor.ID)
	if isActorAbleToRelay(actor) {
		announce := models.NewActivityPubActivity(RelayActor, []string{RelayActor.Followers()}, activity.ID, "Announce")
		jsonData, _ := json.Marshal(&announce)
		go enqueueActivityForAll(actorID.Host, jsonData)
		logrus.Debug("Accepted Announce Activity : ", activity.Actor)
	} else {
		logrus.Debug("Skipped Announce Activity : ", activity.Actor)
	}
	return nil
}
