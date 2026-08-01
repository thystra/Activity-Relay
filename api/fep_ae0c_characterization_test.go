// File: api/fep_ae0c_characterization_test.go

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/models"
)

const fepAE0CPublic = "https://www.w3.org/ns/activitystreams#Public"

type fepAE0CFixtureDocument struct {
	Cases []fepAE0CFixture `json:"cases"`
}

type fepAE0CFixture struct {
	ID          string          `json:"id"`
	Activity    json.RawMessage `json:"activity"`
	Actor       json.RawMessage `json:"actor"`
	HTTPRequest json.RawMessage `json:"http_request"`
	Scenario    json.RawMessage `json:"scenario"`
}

func loadFEPAE0CFixture(t *testing.T, identifier string) fepAE0CFixture {
	t.Helper()

	path := filepath.Join("..", "testdata", "fep-ae0c", "cases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read FEP-ae0c fixture catalog: %v", err)
	}

	var document fepAE0CFixtureDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode FEP-ae0c fixture catalog: %v", err)
	}

	for _, fixture := range document.Cases {
		if fixture.ID == identifier {
			return fixture
		}
	}

	t.Fatalf("FEP-ae0c fixture %q was not found", identifier)
	return fepAE0CFixture{}
}

func fixtureActivity(t *testing.T, identifier string) (*models.Activity, []byte) {
	t.Helper()

	fixture := loadFEPAE0CFixture(t, identifier)
	if len(fixture.Activity) == 0 {
		t.Fatalf("fixture %q has no activity payload", identifier)
	}

	var activity models.Activity
	if err := json.Unmarshal(fixture.Activity, &activity); err != nil {
		t.Fatalf("decode activity fixture %q: %v", identifier, err)
	}

	return &activity, append([]byte(nil), fixture.Activity...)
}

func fixtureActor(t *testing.T, identifier string) *models.Actor {
	t.Helper()

	fixture := loadFEPAE0CFixture(t, identifier)
	if len(fixture.Actor) == 0 {
		t.Fatalf("fixture %q has no actor payload", identifier)
	}

	var actor models.Actor
	if err := json.Unmarshal(fixture.Actor, &actor); err != nil {
		t.Fatalf("decode actor fixture %q: %v", identifier, err)
	}
	return &actor
}

func actorForFixtureActivity(t *testing.T, activity *models.Activity, actorType string) *models.Actor {
	t.Helper()

	actorURL, err := url.Parse(activity.Actor)
	if err != nil || actorURL.Hostname() == "" {
		t.Fatalf("fixture actor URL %q is invalid", activity.Actor)
	}

	inbox := "https://" + actorURL.Host + "/inbox"
	return &models.Actor{
		Context:           []string{"https://www.w3.org/ns/activitystreams"},
		ID:                activity.Actor,
		Type:              actorType,
		PreferredUsername: "fixture",
		Inbox:             inbox,
		Endpoints: &models.Endpoints{
			SharedInbox: inbox,
		},
	}
}

func postFEPAE0CFixture(
	t *testing.T,
	activity *models.Activity,
	actor *models.Actor,
	body []byte,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(
		http.MethodPost,
		"/inbox",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/activity+json")
	recorder := httptest.NewRecorder()

	handleInbox(
		recorder,
		request,
		mockActivityDecoderProvider(activity, actor),
	)
	return recorder
}

func resetFEPAE0CState(t *testing.T, domains ...string) {
	t.Helper()

	removeRelayActivityTestKeys(t)

	ctx := context.Background()
	keys := make([]string, 0, len(domains)*4)
	for _, domain := range domains {
		keys = append(
			keys,
			"relay:subscription:"+domain,
			"relay:follower:"+domain,
			"relay:publisher:"+domain,
			"relay:pending:"+domain,
		)
	}
	if len(keys) > 0 {
		if err := RelayState.RedisClient.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("clear FEP-ae0c state: %v", err)
		}
	}
	if err := RelayState.Load(); err != nil {
		t.Fatalf("reload relay state: %v", err)
	}
}

func addFEPTraditionalReceiver(t *testing.T, domain string) {
	t.Helper()

	RelayState.AddSubscriber(models.Subscriber{
		Domain:   domain,
		InboxURL: "https://" + domain + "/inbox",
		ActorID:  "https://" + domain + "/actor",
	})
}

func waitForFEPStoredBody(t *testing.T, body []byte) {
	t.Helper()

	ctx := context.Background()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		keys, err := models.ScanKeys(
			ctx,
			RelayState.RedisClient,
			"relay:activity:*",
		)
		if err != nil {
			t.Fatalf("scan stored relay activities: %v", err)
		}
		for _, key := range keys {
			stored, err := RelayState.RedisClient.HGet(
				ctx,
				key,
				"body",
			).Bytes()
			if err == nil && bytes.Equal(stored, body) {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("exact fixture body was not stored for relay fan-out")
}

func requireNoFEPRelayActivity(t *testing.T) {
	t.Helper()

	time.Sleep(250 * time.Millisecond)
	keys, err := models.ScanKeys(
		context.Background(),
		RelayState.RedisClient,
		"relay:activity:*",
	)
	if err != nil {
		t.Fatalf("scan stored relay activities: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("unexpected relay activity keys: %v", keys)
	}
}

func followerMutualStatus(domain string) (bool, bool) {
	for _, follower := range RelayState.Snapshot().Followers {
		if follower.Domain == domain {
			return follower.MutuallyFollow, true
		}
	}
	return false, false
}

func TestFEPAE0CTraditionalFollowFixtures(t *testing.T) {
	for _, identifier := range []string{
		"mastodon-follow-public",
		"historical-state-property",
	} {
		t.Run(identifier, func(t *testing.T) {
			const domain = "subscriber.example"
			resetFEPAE0CState(t, domain)
			t.Cleanup(func() {
				resetFEPAE0CState(t, domain)
			})

			activity, body := fixtureActivity(t, identifier)
			actor := actorForFixtureActivity(t, activity, "Application")
			recorder := postFEPAE0CFixture(t, activity, actor, body)

			if recorder.Code != http.StatusAccepted {
				t.Fatalf(
					"status = %d; want %d",
					recorder.Code,
					http.StatusAccepted,
				)
			}
			if !RelayState.IsSubscriber(domain) {
				t.Fatalf(
					"traditional fixture %q did not create subscriber %s",
					identifier,
					domain,
				)
			}
		})
	}
}

func TestFEPAE0CLitePubFollowAndAcceptFixtures(t *testing.T) {
	const domain = "follower.example"

	t.Run("follow", func(t *testing.T) {
		resetFEPAE0CState(t, domain)
		t.Cleanup(func() {
			resetFEPAE0CState(t, domain)
		})

		activity, body := fixtureActivity(
			t,
			"litepub-follow-relay-actor",
		)
		activity.Object = RelayActor.ID
		activity.To = []string{RelayActor.ID}
		actor := actorForFixtureActivity(t, activity, "Application")

		recorder := postFEPAE0CFixture(t, activity, actor, body)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf(
				"status = %d; want %d",
				recorder.Code,
				http.StatusAccepted,
			)
		}
		if !RelayState.IsFollower(domain) {
			t.Fatalf("LitePub follow did not create follower %s", domain)
		}
		mutual, found := followerMutualStatus(domain)
		if !found {
			t.Fatalf("follower %s is absent from snapshot", domain)
		}
		if mutual {
			t.Fatalf("follower %s became mutual before Accept", domain)
		}
	})

	t.Run("accept reciprocal follow", func(t *testing.T) {
		resetFEPAE0CState(t, domain)
		t.Cleanup(func() {
			resetFEPAE0CState(t, domain)
		})

		activity, body := fixtureActivity(
			t,
			"litepub-accept-reciprocal-follow",
		)
		actor := actorForFixtureActivity(t, activity, "Application")
		RelayState.AddFollower(models.Follower{
			Domain:         domain,
			InboxURL:       actor.Inbox,
			ActivityID:     "https://relay.example/activities/follow/1",
			ActorID:        actor.ID,
			MutuallyFollow: false,
		})

		activity.To = []string{RelayActor.ID}
		inner, ok := activity.Object.(map[string]interface{})
		if !ok {
			t.Fatal("Accept fixture object is not an embedded activity")
		}
		inner["actor"] = RelayActor.ID
		inner["object"] = actor.ID

		recorder := postFEPAE0CFixture(t, activity, actor, body)
		if recorder.Code != http.StatusAccepted {
			t.Fatalf(
				"status = %d; want %d",
				recorder.Code,
				http.StatusAccepted,
			)
		}
		mutual, found := followerMutualStatus(domain)
		if !found {
			t.Fatalf("follower %s is absent from snapshot", domain)
		}
		if !mutual {
			t.Fatalf("follower %s was not finalized as mutual", domain)
		}
	})
}

func TestFEPAE0CTraditionalPublicAudienceFixtures(t *testing.T) {
	for _, identifier := range []string{
		"traditional-create-public-to",
		"traditional-create-public-cc",
	} {
		t.Run(identifier, func(t *testing.T) {
			const (
				sourceDomain   = "publisher.example"
				receiverDomain = "receiver.example"
			)
			resetFEPAE0CState(
				t,
				sourceDomain,
				receiverDomain,
			)
			t.Cleanup(func() {
				resetFEPAE0CState(
					t,
					sourceDomain,
					receiverDomain,
				)
			})
			addFEPTraditionalReceiver(t, receiverDomain)

			activity, body := fixtureActivity(t, identifier)
			actor := actorForFixtureActivity(t, activity, "Person")
			recorder := postFEPAE0CFixture(t, activity, actor, body)

			if recorder.Code != http.StatusAccepted {
				t.Fatalf(
					"status = %d; want %d",
					recorder.Code,
					http.StatusAccepted,
				)
			}
			waitForFEPStoredBody(t, body)
			if !RelayState.IsPublisher(sourceDomain) {
				t.Fatalf("source %s was not recorded as publisher", sourceDomain)
			}
		})
	}
}

func TestFEPAE0CPreservesOriginalBodyWithLinkedDataProof(t *testing.T) {
	const (
		sourceDomain   = "publisher.example"
		receiverDomain = "receiver.example"
	)
	resetFEPAE0CState(t, sourceDomain, receiverDomain)
	t.Cleanup(func() {
		resetFEPAE0CState(t, sourceDomain, receiverDomain)
	})
	addFEPTraditionalReceiver(t, receiverDomain)

	activity, body := fixtureActivity(
		t,
		"traditional-forward-preserves-ld-proof",
	)
	if !bytes.Contains(body, []byte(`"RsaSignature2017"`)) {
		t.Fatal("LD-proof fixture does not contain RsaSignature2017")
	}
	actor := actorForFixtureActivity(t, activity, "Person")
	recorder := postFEPAE0CFixture(t, activity, actor, body)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf(
			"status = %d; want %d",
			recorder.Code,
			http.StatusAccepted,
		)
	}
	waitForFEPStoredBody(t, body)
}

func TestFEPAE0CPublicAnnounceNonFanoutFixtures(t *testing.T) {
	for _, identifier := range []string{
		"public-announce-url-only",
		"public-announce-cross-origin-embedded",
	} {
		t.Run(identifier, func(t *testing.T) {
			const (
				sourceDomain   = "publisher.example"
				receiverDomain = "receiver.example"
			)
			resetFEPAE0CState(
				t,
				sourceDomain,
				receiverDomain,
			)
			t.Cleanup(func() {
				resetFEPAE0CState(
					t,
					sourceDomain,
					receiverDomain,
				)
			})
			addFEPTraditionalReceiver(t, receiverDomain)

			activity, body := fixtureActivity(t, identifier)
			actor := actorForFixtureActivity(t, activity, "Person")
			recorder := postFEPAE0CFixture(t, activity, actor, body)

			if recorder.Code != http.StatusAccepted {
				t.Fatalf(
					"status = %d; want %d",
					recorder.Code,
					http.StatusAccepted,
				)
			}
			requireNoFEPRelayActivity(t)
			publisher := RelayState.SelectPublisher(sourceDomain)
			if publisher == nil {
				t.Fatalf("source %s was not recorded as publisher", sourceDomain)
			}
			if publisher.LastActivityType != "Announce" {
				t.Fatalf(
					"last publisher activity = %q; want Announce",
					publisher.LastActivityType,
				)
			}
		})
	}
}

func TestFEPAE0CUnsupportedPublicLikeIsAcknowledgedWithoutFanout(t *testing.T) {
	const (
		sourceDomain   = "publisher.example"
		receiverDomain = "receiver.example"
	)
	resetFEPAE0CState(t, sourceDomain, receiverDomain)
	t.Cleanup(func() {
		resetFEPAE0CState(t, sourceDomain, receiverDomain)
	})
	addFEPTraditionalReceiver(t, receiverDomain)

	activity, body := fixtureActivity(t, "unsupported-public-like")
	actor := actorForFixtureActivity(t, activity, "Person")
	recorder := postFEPAE0CFixture(t, activity, actor, body)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf(
			"status = %d; want %d",
			recorder.Code,
			http.StatusAccepted,
		)
	}
	requireNoFEPRelayActivity(t)
	if RelayState.IsPublisher(sourceDomain) {
		t.Fatalf(
			"unsupported public Like unexpectedly recorded publisher %s",
			sourceDomain,
		)
	}
}

func TestFEPAE0COpenPublisherCreate(t *testing.T) {
	const (
		sourceDomain   = "blog.example"
		receiverDomain = "receiver.example"
	)
	resetFEPAE0CState(t, sourceDomain, receiverDomain)
	t.Cleanup(func() {
		resetFEPAE0CState(t, sourceDomain, receiverDomain)
	})
	addFEPTraditionalReceiver(t, receiverDomain)

	activity, body := fixtureActivity(t, "open-publisher-public-create")
	actor := actorForFixtureActivity(t, activity, "Person")
	recorder := postFEPAE0CFixture(t, activity, actor, body)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf(
			"status = %d; want %d",
			recorder.Code,
			http.StatusAccepted,
		)
	}
	if RelayState.IsSubscriber(sourceDomain) ||
		RelayState.IsFollower(sourceDomain) {
		t.Fatalf(
			"open publisher %s unexpectedly became a receiver",
			sourceDomain,
		)
	}
	waitForFEPStoredBody(t, body)
	if !RelayState.IsPublisher(sourceDomain) {
		t.Fatalf("open publisher %s was not recorded", sourceDomain)
	}
}

func TestFEPAE0CServerActorArbitraryPath(t *testing.T) {
	actor := fixtureActor(t, "server-actor-arbitrary-path")
	if !isActorAbleToBeFollower(actor) {
		t.Fatalf(
			"Application actor at implementation-defined path %q was rejected",
			actor.ID,
		)
	}
}

func TestFEPAE0CNodeBBNormalizationGuardFixtures(t *testing.T) {
	tests := []struct {
		identifier string
		want       bool
	}{
		{
			identifier: "nodebb-embedded-announce-same-origin",
			want:       true,
		},
		{
			identifier: "public-announce-url-only",
			want:       false,
		},
		{
			identifier: "public-announce-cross-origin-embedded",
			want:       false,
		},
	}

	for _, test := range tests {
		t.Run(test.identifier, func(t *testing.T) {
			activity, _ := fixtureActivity(t, test.identifier)
			if got := shouldFanOutPublicAnnounce(activity); got != test.want {
				t.Fatalf(
					"shouldFanOutPublicAnnounce(%s) = %v; want %v",
					test.identifier,
					got,
					test.want,
				)
			}
		})
	}
}

func TestFEPAE0CFixturePublicURIConstant(t *testing.T) {
	activity, _ := fixtureActivity(t, "traditional-create-public-to")
	if !contains(activity.To, fepAE0CPublic) {
		t.Fatalf(
			"fixture does not address expected Public URI %q",
			fepAE0CPublic,
		)
	}
}

// EOF: api/fep_ae0c_characterization_test.go
