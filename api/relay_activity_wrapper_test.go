// File: api/relay_activity_wrapper_test.go

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/models"
)

func relayWrapperTestActor(sourceDomain string) *models.Actor {
	return &models.Actor{
		ID:    "https://" + sourceDomain + "/?author=0",
		Type:  "Group",
		Inbox: "https://" + sourceDomain + "/inbox",
		Endpoints: &models.Endpoints{
			SharedInbox: "https://" + sourceDomain + "/shared-inbox",
		},
	}
}

func relayWrapperTestActivity(
	activityType string,
	sourceDomain string,
	objectID string,
) *models.Activity {
	objectType := "Article"
	if activityType == "Delete" {
		objectType = "Tombstone"
	}

	return &models.Activity{
		Context: []string{
			"https://www.w3.org/ns/activitystreams",
		},
		ID: fmt.Sprintf(
			"https://%s/activities/%s",
			sourceDomain,
			activityType,
		),
		Actor: "https://" + sourceDomain + "/?author=0",
		Type:  activityType,
		Object: map[string]interface{}{
			"id":   objectID,
			"type": objectType,
		},
		To: []string{
			"https://www.w3.org/ns/activitystreams#Public",
		},
	}
}

func relayWrapperSignedBody(
	t *testing.T,
	activity *models.Activity,
) []byte {
	t.Helper()

	plain, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal activity: %v", err)
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(plain, &envelope); err != nil {
		t.Fatalf("decode activity envelope: %v", err)
	}
	envelope["signature"] = map[string]interface{}{
		"type":           "RsaSignature2017",
		"creator":        activity.Actor + "#main-key",
		"created":        "2026-08-03T20:00:00Z",
		"signatureValue": "fixture-not-a-real-signature",
	}

	signed, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal signed activity: %v", err)
	}
	return signed
}

func relayStoredActivities(
	t *testing.T,
) map[string][]byte {
	t.Helper()

	ctx := context.Background()
	keys, err := models.ScanKeys(
		ctx,
		RelayState.RedisClient,
		"relay:activity:*",
	)
	if err != nil {
		t.Fatalf("scan relay activities: %v", err)
	}

	result := make(map[string][]byte, len(keys))
	for _, key := range keys {
		body, err := RelayState.RedisClient.
			HGet(ctx, key, "body").
			Bytes()
		if err != nil {
			continue
		}
		result[key] = body
	}
	return result
}

func waitForRelayWrapper(
	t *testing.T,
	objectID string,
) (models.Activity, int, bool) {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		for key, storedBody := range relayStoredActivities(t) {
			var relayed models.Activity
			if err := json.Unmarshal(storedBody, &relayed); err != nil {
				continue
			}
			if relayed.Type != "Announce" ||
				relayed.Actor != RelayActor.ID ||
				relayed.Object != objectID {
				continue
			}

			targetCount, err := RelayState.RedisClient.
				HGet(ctx, key, "remain_count").
				Int()
			if err != nil {
				t.Fatalf(
					"read relay target count for %s: %v",
					key,
					err,
				)
			}
			return relayed, targetCount, true
		}

		time.Sleep(25 * time.Millisecond)
	}

	return models.Activity{}, 0, false
}

func waitForStoredRelayBody(
	t *testing.T,
	expected []byte,
) (int, bool) {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		for key, storedBody := range relayStoredActivities(t) {
			if !bytes.Equal(storedBody, expected) {
				continue
			}

			targetCount, err := RelayState.RedisClient.
				HGet(ctx, key, "remain_count").
				Int()
			if err != nil {
				t.Fatalf(
					"read relay target count for %s: %v",
					key,
					err,
				)
			}
			return targetCount, true
		}

		time.Sleep(25 * time.Millisecond)
	}

	return 0, false
}

func storedRelayBodyEquals(
	t *testing.T,
	expected []byte,
) bool {
	t.Helper()

	for _, storedBody := range relayStoredActivities(t) {
		if bytes.Equal(storedBody, expected) {
			return true
		}
	}
	return false
}

func storedRelayWrapperExists(
	t *testing.T,
	objectID string,
) bool {
	t.Helper()

	for _, storedBody := range relayStoredActivities(t) {
		var relayed models.Activity
		if err := json.Unmarshal(storedBody, &relayed); err != nil {
			continue
		}
		if relayed.Type == "Announce" &&
			relayed.Actor == RelayActor.ID &&
			relayed.Object == objectID {
			return true
		}
	}
	return false
}

func assertRelayPublisherType(
	t *testing.T,
	sourceDomain string,
	activityType string,
) {
	t.Helper()

	publisher := RelayState.SelectPublisher(sourceDomain)
	if publisher == nil {
		t.Fatal("source was not recorded as a publisher")
	}
	if publisher.LastActivityType != activityType {
		t.Fatalf(
			"publisher activity type = %q; want %q",
			publisher.LastActivityType,
			activityType,
		)
	}
}

func TestExecuteRelayActivityWrapsUnsignedPublicActivitiesForAllReceivers(
	t *testing.T,
) {
	const (
		sourceDomain      = "wordpress-wrapper.example"
		traditionalDomain = "mastodon-wrapper.example"
		followerDomain    = "friendica-wrapper.example"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(
		ctx,
		"relay:publisher:"+sourceDomain,
	)

	RelayState.AddSubscriber(models.Subscriber{
		Domain:   traditionalDomain,
		InboxURL: "https://" + traditionalDomain + "/inbox",
	})
	RelayState.AddFollower(models.Follower{
		Domain:         followerDomain,
		InboxURL:       "https://" + followerDomain + "/inbox",
		ActorID:        "https://" + followerDomain + "/actor",
		MutuallyFollow: true,
	})

	t.Cleanup(func() {
		RelayState.DelSubscriber(traditionalDomain)
		RelayState.DelFollower(followerDomain)
		RelayState.RedisClient.Del(
			ctx,
			"relay:publisher:"+sourceDomain,
		)
		removeRelayActivityTestKeys(t)
	})

	for _, activityType := range []string{
		"Create",
		"Update",
		"Delete",
		"Move",
	} {
		t.Run(activityType, func(t *testing.T) {
			removeRelayActivityTestKeys(t)

			objectID := fmt.Sprintf(
				"https://%s/objects/%s",
				sourceDomain,
				activityType,
			)
			activity := relayWrapperTestActivity(
				activityType,
				sourceDomain,
				objectID,
			)
			plainBody, err := json.Marshal(activity)
			if err != nil {
				t.Fatalf(
					"marshal %s activity: %v",
					activityType,
					err,
				)
			}

			if err := executeRelayActivity(
				activity,
				relayWrapperTestActor(sourceDomain),
				plainBody,
			); err != nil {
				t.Fatalf(
					"executeRelayActivity(%s): %v",
					activityType,
					err,
				)
			}

			relayed, targetCount, found := waitForRelayWrapper(
				t,
				objectID,
			)
			if !found {
				t.Fatalf(
					"relay-authored Announce for %s was not queued",
					activityType,
				)
			}
			if relayed.ID == "" {
				t.Fatal("relay-authored Announce has no ID")
			}
			if !contains(
				relayed.To,
				RelayActor.Followers(),
			) {
				t.Fatalf(
					"relay Announce targets %v; want %q",
					relayed.To,
					RelayActor.Followers(),
				)
			}
			if targetCount != 2 {
				t.Fatalf(
					"relay Announce target count = %d; want 2",
					targetCount,
				)
			}
			if storedRelayBodyEquals(t, plainBody) {
				t.Fatalf(
					"unsigned %s body must not be queued",
					activityType,
				)
			}
			assertRelayPublisherType(
				t,
				sourceDomain,
				activityType,
			)
		})
	}
}

func TestExecuteRelayActivityPreservesLDSignatureForSubscriberAndWrapsFollower(
	t *testing.T,
) {
	const (
		sourceDomain      = "signed-publisher.example"
		traditionalDomain = "signed-mastodon.example"
		followerDomain    = "signed-follower.example"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(
		ctx,
		"relay:publisher:"+sourceDomain,
	)
	RelayState.AddSubscriber(models.Subscriber{
		Domain:   traditionalDomain,
		InboxURL: "https://" + traditionalDomain + "/inbox",
	})
	RelayState.AddFollower(models.Follower{
		Domain:         followerDomain,
		InboxURL:       "https://" + followerDomain + "/inbox",
		ActorID:        "https://" + followerDomain + "/actor",
		MutuallyFollow: true,
	})

	t.Cleanup(func() {
		RelayState.DelSubscriber(traditionalDomain)
		RelayState.DelFollower(followerDomain)
		RelayState.RedisClient.Del(
			ctx,
			"relay:publisher:"+sourceDomain,
		)
		removeRelayActivityTestKeys(t)
	})

	objectID := "https://" + sourceDomain + "/objects/signed"
	activity := relayWrapperTestActivity(
		"Create",
		sourceDomain,
		objectID,
	)
	signedBody := relayWrapperSignedBody(t, activity)

	if err := executeRelayActivity(
		activity,
		relayWrapperTestActor(sourceDomain),
		signedBody,
	); err != nil {
		t.Fatalf("executeRelayActivity: %v", err)
	}

	directTargets, found := waitForStoredRelayBody(t, signedBody)
	if !found {
		t.Fatal("exact LD-signed body was not queued")
	}
	if directTargets != 1 {
		t.Fatalf(
			"direct LD-signed target count = %d; want 1",
			directTargets,
		)
	}

	_, wrapperTargets, found := waitForRelayWrapper(t, objectID)
	if !found {
		t.Fatal("follower relay Announce was not queued")
	}
	if wrapperTargets != 1 {
		t.Fatalf(
			"follower wrapper target count = %d; want 1",
			wrapperTargets,
		)
	}
	assertRelayPublisherType(t, sourceDomain, "Create")
}

func TestExecuteRelayActivityDeduplicatesUnsignedReceiverStylesAndExcludesSource(
	t *testing.T,
) {
	const (
		sourceDomain = "source-overlap.example"
		targetDomain = "receiver-overlap.example"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(
		ctx,
		"relay:publisher:"+sourceDomain,
	)

	RelayState.AddSubscriber(models.Subscriber{
		Domain:   sourceDomain,
		InboxURL: "https://" + sourceDomain + "/inbox",
	})
	RelayState.AddSubscriber(models.Subscriber{
		Domain:   targetDomain,
		InboxURL: "https://" + targetDomain + "/shared-inbox",
	})
	RelayState.AddFollower(models.Follower{
		Domain:         targetDomain,
		InboxURL:       "https://" + targetDomain + "/actor/inbox",
		ActorID:        "https://" + targetDomain + "/actor",
		MutuallyFollow: true,
	})

	t.Cleanup(func() {
		RelayState.DelSubscriber(sourceDomain)
		RelayState.DelSubscriber(targetDomain)
		RelayState.DelFollower(targetDomain)
		RelayState.RedisClient.Del(
			ctx,
			"relay:publisher:"+sourceDomain,
		)
		removeRelayActivityTestKeys(t)
	})

	objectID := "https://" + sourceDomain + "/objects/overlap"
	activity := relayWrapperTestActivity(
		"Create",
		sourceDomain,
		objectID,
	)
	plainBody, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal overlap activity: %v", err)
	}

	if err := executeRelayActivity(
		activity,
		relayWrapperTestActor(sourceDomain),
		plainBody,
	); err != nil {
		t.Fatalf("executeRelayActivity: %v", err)
	}

	_, targetCount, found := waitForRelayWrapper(t, objectID)
	if !found {
		t.Fatal("relay-authored Announce was not queued")
	}
	if targetCount != 1 {
		t.Fatalf(
			"deduplicated target count = %d; want 1",
			targetCount,
		)
	}
	if storedRelayBodyEquals(t, plainBody) {
		t.Fatal("unsigned source activity must not be queued")
	}
}

func TestExecuteRelayActivitySignedOverlapPrefersTraditionalRoute(
	t *testing.T,
) {
	const (
		sourceDomain = "signed-source-overlap.example"
		targetDomain = "signed-receiver-overlap.example"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(
		ctx,
		"relay:publisher:"+sourceDomain,
	)
	RelayState.AddSubscriber(models.Subscriber{
		Domain:   targetDomain,
		InboxURL: "https://" + targetDomain + "/shared-inbox",
	})
	RelayState.AddFollower(models.Follower{
		Domain:         targetDomain,
		InboxURL:       "https://" + targetDomain + "/actor/inbox",
		ActorID:        "https://" + targetDomain + "/actor",
		MutuallyFollow: true,
	})

	t.Cleanup(func() {
		RelayState.DelSubscriber(targetDomain)
		RelayState.DelFollower(targetDomain)
		RelayState.RedisClient.Del(
			ctx,
			"relay:publisher:"+sourceDomain,
		)
		removeRelayActivityTestKeys(t)
	})

	objectID := "https://" + sourceDomain + "/objects/signed-overlap"
	activity := relayWrapperTestActivity(
		"Create",
		sourceDomain,
		objectID,
	)
	signedBody := relayWrapperSignedBody(t, activity)

	if err := executeRelayActivity(
		activity,
		relayWrapperTestActor(sourceDomain),
		signedBody,
	); err != nil {
		t.Fatalf("executeRelayActivity: %v", err)
	}

	targetCount, found := waitForStoredRelayBody(t, signedBody)
	if !found {
		t.Fatal("exact signed body was not queued")
	}
	if targetCount != 1 {
		t.Fatalf(
			"signed overlap target count = %d; want 1",
			targetCount,
		)
	}

	time.Sleep(250 * time.Millisecond)
	if keys := len(relayStoredActivities(t)); keys != 1 {
		t.Fatalf(
			"signed overlap created %d stored payloads; want 1",
			keys,
		)
	}
	if storedRelayWrapperExists(t, objectID) {
		t.Fatal(
			"overlapping follower route produced duplicate Announce",
		)
	}
}

func TestExecuteRelayActivityUnsignedMissingObjectIDFailsClosed(
	t *testing.T,
) {
	const (
		sourceDomain = "missing-object-id.example"
		targetDomain = "subscriber-missing-id.example"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(
		ctx,
		"relay:publisher:"+sourceDomain,
	)
	RelayState.AddSubscriber(models.Subscriber{
		Domain:   targetDomain,
		InboxURL: "https://" + targetDomain + "/inbox",
	})

	t.Cleanup(func() {
		RelayState.DelSubscriber(targetDomain)
		RelayState.RedisClient.Del(
			ctx,
			"relay:publisher:"+sourceDomain,
		)
		removeRelayActivityTestKeys(t)
	})

	activity := relayWrapperTestActivity(
		"Update",
		sourceDomain,
		"https://"+sourceDomain+"/objects/missing",
	)
	activity.Object = map[string]interface{}{
		"type": "Article",
	}

	plainBody, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal missing-ID activity: %v", err)
	}

	if err := executeRelayActivity(
		activity,
		relayWrapperTestActor(sourceDomain),
		plainBody,
	); err != nil {
		t.Fatalf("executeRelayActivity: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if keys := len(relayStoredActivities(t)); keys != 0 {
		t.Fatalf(
			"unsigned missing-ID activity created %d payloads; want 0",
			keys,
		)
	}
	assertRelayPublisherType(t, sourceDomain, "Update")
}

func TestExecuteRelayActivitySignedMissingObjectIDForwardsSubscriberOnly(
	t *testing.T,
) {
	const (
		sourceDomain      = "signed-missing-id.example"
		traditionalDomain = "signed-missing-id-subscriber.example"
		followerDomain    = "signed-missing-id-follower.example"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(
		ctx,
		"relay:publisher:"+sourceDomain,
	)
	RelayState.AddSubscriber(models.Subscriber{
		Domain:   traditionalDomain,
		InboxURL: "https://" + traditionalDomain + "/inbox",
	})
	RelayState.AddFollower(models.Follower{
		Domain:         followerDomain,
		InboxURL:       "https://" + followerDomain + "/inbox",
		ActorID:        "https://" + followerDomain + "/actor",
		MutuallyFollow: true,
	})

	t.Cleanup(func() {
		RelayState.DelSubscriber(traditionalDomain)
		RelayState.DelFollower(followerDomain)
		RelayState.RedisClient.Del(
			ctx,
			"relay:publisher:"+sourceDomain,
		)
		removeRelayActivityTestKeys(t)
	})

	activity := relayWrapperTestActivity(
		"Update",
		sourceDomain,
		"https://"+sourceDomain+"/objects/missing",
	)
	activity.Object = map[string]interface{}{
		"type": "Article",
	}
	signedBody := relayWrapperSignedBody(t, activity)

	if err := executeRelayActivity(
		activity,
		relayWrapperTestActor(sourceDomain),
		signedBody,
	); err != nil {
		t.Fatalf("executeRelayActivity: %v", err)
	}

	targetCount, found := waitForStoredRelayBody(t, signedBody)
	if !found {
		t.Fatal("signed missing-ID body was not queued")
	}
	if targetCount != 1 {
		t.Fatalf(
			"signed missing-ID target count = %d; want 1",
			targetCount,
		)
	}

	time.Sleep(250 * time.Millisecond)
	if keys := len(relayStoredActivities(t)); keys != 1 {
		t.Fatalf(
			"signed missing-ID activity created %d payloads; want 1",
			keys,
		)
	}
	assertRelayPublisherType(t, sourceDomain, "Update")
}

// EOF: api/relay_activity_wrapper_test.go
