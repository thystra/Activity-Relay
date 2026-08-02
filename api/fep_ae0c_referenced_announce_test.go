// File: api/fep_ae0c_referenced_announce_test.go

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-fed/httpsig"
	"github.com/thystra/Activity-Relay/models"
)

func verifyFEPAE0CSignedGET(request *http.Request) error {
	if request.Method != http.MethodGet {
		return fmt.Errorf(
			"remote request method = %s; want GET",
			request.Method,
		)
	}
	if request.Host == "" {
		return fmt.Errorf("signed remote GET has no wire Host authority")
	}
	if request.Header.Get("Date") == "" {
		return fmt.Errorf("signed remote GET has no Date header")
	}
	if request.Header.Get("Digest") != "" {
		return fmt.Errorf(
			"signed remote GET unexpectedly has a Digest header",
		)
	}
	if request.Header.Get("Signature") == "" {
		return fmt.Errorf("remote GET has no Signature header")
	}

	publicKey, err := models.ReadPublicKeyRSAFromString(
		RelayActor.PublicKey.PublicKeyPem,
	)
	if err != nil {
		return fmt.Errorf("parse relay public key: %w", err)
	}
	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		return fmt.Errorf("create remote GET verifier: %w", err)
	}
	if verifier.KeyId() != RelayActor.PublicKey.ID {
		return fmt.Errorf(
			"remote GET key ID = %q; want %q",
			verifier.KeyId(),
			RelayActor.PublicKey.ID,
		)
	}
	if err := verifier.Verify(publicKey, httpsig.RSA_SHA256); err != nil {
		return fmt.Errorf("verify remote GET signature: %w", err)
	}
	return nil
}

func waitForFEPAE0CRelayAnnounce(
	t *testing.T,
	objectID string,
	wantTargets int,
) {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		keys, err := models.ScanKeys(
			ctx,
			RelayState.RedisClient,
			"relay:activity:*",
		)
		if err != nil {
			t.Fatalf("scan relay activity keys: %v", err)
		}
		for _, key := range keys {
			body, err := RelayState.RedisClient.HGet(
				ctx,
				key,
				"body",
			).Bytes()
			if err != nil {
				continue
			}
			var activity models.Activity
			if err := json.Unmarshal(body, &activity); err != nil {
				continue
			}
			if activity.Type != "Announce" ||
				activity.Actor != RelayActor.ID ||
				activity.Object != objectID {
				continue
			}
			remaining, err := RelayState.RedisClient.HGet(
				ctx,
				key,
				"remain_count",
			).Int()
			if err != nil {
				t.Fatalf("read relay target count: %v", err)
			}
			if remaining != wantTargets {
				t.Fatalf(
					"relay Announce target count = %d; want %d",
					remaining,
					wantTargets,
				)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf(
		"relay-authored Announce for %q was not queued",
		objectID,
	)
}

func resetFEPAE0CReferencedAnnounceState(t *testing.T) {
	t.Helper()

	removeRelayActivityTestKeys(t)

	ctx := context.Background()
	patterns := []string{
		"relay:subscription:*",
		"relay:follower:*",
		"relay:publisher:*",
		"relay:pending:*",
		canonicalRelayKeyPrefix + "*",
	}
	keys := make([]string, 0)
	for _, pattern := range patterns {
		matched, err := models.ScanKeys(
			ctx,
			RelayState.RedisClient,
			pattern,
		)
		if err != nil {
			t.Fatalf(
				"scan FEP-ae0c state pattern %q: %v",
				pattern,
				err,
			)
		}
		keys = append(keys, matched...)
	}
	if len(keys) > 0 {
		if err := RelayState.RedisClient.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("clear FEP-ae0c receiver state: %v", err)
		}
	}
	if err := RelayState.Load(); err != nil {
		t.Fatalf("reload relay state: %v", err)
	}
}

func TestFEPAE0CReferencedAnnounceFetchesSignedRemoteActivityAndQueuesRelayAnnounce(
	t *testing.T,
) {
	const (
		announcerDomain = "follower.example"
		receiverDomain  = "receiver.example"
	)

	var signedGETs atomic.Int64
	var originActor models.Actor
	var originActivity models.Activity

	origin := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if err := verifyFEPAE0CSignedGET(request); err != nil {
				t.Errorf("verify signed remote GET: %v", err)
				http.Error(
					writer,
					err.Error(),
					http.StatusUnauthorized,
				)
				return
			}
			signedGETs.Add(1)

			writer.Header().Set(
				"Content-Type",
				"application/activity+json",
			)
			switch request.URL.Path {
			case "/actor":
				if err := json.NewEncoder(writer).Encode(&originActor); err != nil {
					t.Errorf("encode origin actor: %v", err)
				}
			case "/activities/create-1":
				if err := json.NewEncoder(writer).Encode(&originActivity); err != nil {
					t.Errorf("encode origin activity: %v", err)
				}
			default:
				http.NotFound(writer, request)
			}
		},
	))
	defer origin.Close()

	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatalf("parse origin server URL: %v", err)
	}
	originActor = models.Actor{
		Context:           []string{"https://www.w3.org/ns/activitystreams"},
		ID:                origin.URL + "/actor",
		Type:              "Person",
		PreferredUsername: "origin",
		Inbox:             origin.URL + "/inbox",
		Endpoints: &models.Endpoints{
			SharedInbox: origin.URL + "/inbox",
		},
	}
	originActivity = models.Activity{
		Context: []string{"https://www.w3.org/ns/activitystreams"},
		ID:      origin.URL + "/activities/create-1",
		Type:    "Create",
		Actor:   originActor.ID,
		Object: map[string]interface{}{
			"id":           origin.URL + "/objects/1",
			"type":         "Note",
			"attributedTo": originActor.ID,
			"content":      "Referenced Announce integration fixture",
		},
		To: []string{fepAE0CPublic},
	}

	resetFEPAE0CReferencedAnnounceState(t)
	t.Cleanup(func() {
		resetFEPAE0CReferencedAnnounceState(t)
		ActorCache.Delete(originActor.ID)
	})

	RelayState.AddFollower(models.Follower{
		Domain:         announcerDomain,
		InboxURL:       "https://follower.example/inbox",
		ActivityID:     "https://follower.example/activities/follow-relay",
		ActorID:        "https://follower.example/actor",
		MutuallyFollow: true,
	})
	addFEPTraditionalReceiver(t, receiverDomain)

	activity, _ := fixtureActivity(t, "litepub-announce-reference")
	activity.Actor = "https://follower.example/actor"
	activity.Object = originActivity.ID
	activity.To = []string{RelayActor.ID}
	activity.Cc = nil

	outerActor := actorForFixtureActivity(
		t,
		activity,
		"Application",
	)
	body, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal referenced Announce fixture: %v", err)
	}
	recorder := postFEPAE0CFixture(
		t,
		activity,
		outerActor,
		body,
	)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf(
			"status = %d, body = %q; want %d",
			recorder.Code,
			strings.TrimSpace(recorder.Body.String()),
			http.StatusAccepted,
		)
	}

	waitForFEPAE0CRelayAnnounce(
		t,
		originActivity.ID,
		1,
	)

	duplicateRecorder := postFEPAE0CFixture(
		t,
		activity,
		outerActor,
		body,
	)
	if duplicateRecorder.Code != http.StatusAccepted {
		t.Fatalf(
			"duplicate status = %d, body = %q; want %d",
			duplicateRecorder.Code,
			strings.TrimSpace(duplicateRecorder.Body.String()),
			http.StatusAccepted,
		)
	}
	time.Sleep(250 * time.Millisecond)
	activityKeys, err := models.ScanKeys(
		context.Background(),
		RelayState.RedisClient,
		"relay:activity:*",
	)
	if err != nil {
		t.Fatalf("scan relay activities after duplicate: %v", err)
	}
	if len(activityKeys) != 1 {
		t.Fatalf(
			"relay activity count after duplicate = %d; want 1",
			len(activityKeys),
		)
	}
	if signedGETs.Load() < 3 {
		t.Fatalf(
			"signed remote GET count = %d; want at least 3 across two requests",
			signedGETs.Load(),
		)
	}

	publisher := RelayState.SelectPublisher(originURL.Hostname())
	if publisher == nil {
		t.Fatalf(
			"origin domain %q was not recorded as a publisher",
			originURL.Hostname(),
		)
	}
	if publisher.LastActivityID != originActivity.ID {
		t.Fatalf(
			"publisher last activity ID = %q; want %q",
			publisher.LastActivityID,
			originActivity.ID,
		)
	}
	if publisher.LastActivityType != "Create" {
		t.Fatalf(
			"publisher last activity type = %q; want Create",
			publisher.LastActivityType,
		)
	}
}

// EOF: api/fep_ae0c_referenced_announce_test.go
