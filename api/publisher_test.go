package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/thystra/Activity-Relay/models"
)

func TestHandleInboxBlockedPublisherCreate(t *testing.T) {
	activity := mockActivity("Create")
	actor := mockActor("Person")
	domain, _ := url.Parse(activity.Actor)
	RelayState.RedisClient.Del(context.TODO(), "relay:publisher:"+domain.Hostname()).Result()
	RelayState.Load()
	RelayState.SetBlockedDomain(domain.Hostname(), true)
	t.Cleanup(func() {
		RelayState.SetBlockedDomain(domain.Hostname(), false)
		RelayState.RedisClient.Del(context.TODO(), "relay:publisher:"+domain.Hostname()).Result()
		RelayState.Load()
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/inbox", nil)
	handleInbox(recorder, request, mockActivityDecoderProvider(&activity, &actor))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status code = %d; want %d", recorder.Code, http.StatusUnauthorized)
	}
	if RelayState.IsPublisher(domain.Hostname()) {
		t.Fatalf("blocked domain %s was recorded as a publisher", domain.Hostname())
	}
}

func TestPublicAnnounceRecordsPublisher(t *testing.T) {
	activity := mockActivity("Create")
	activity.Type = "Announce"
	actor := mockActor("Person")
	domain, _ := url.Parse(activity.Actor)
	RelayState.RedisClient.Del(context.TODO(), "relay:publisher:"+domain.Hostname()).Result()
	RelayState.Load()
	t.Cleanup(func() {
		RelayState.RedisClient.Del(context.TODO(), "relay:publisher:"+domain.Hostname()).Result()
		RelayState.Load()
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/inbox", nil)
	handleInbox(recorder, request, mockActivityDecoderProvider(&activity, &actor))

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status code = %d; want %d", recorder.Code, http.StatusAccepted)
	}
	publisher := RelayState.SelectPublisher(domain.Hostname())
	if publisher == nil {
		t.Fatalf("publisher %s was not recorded", domain.Hostname())
	}
	if publisher.LastActivityType != "Announce" {
		t.Fatalf("last activity type = %q; want Announce", publisher.LastActivityType)
	}
}

func TestVerifySignatureActorBinding(t *testing.T) {
	wordpressActor := "https://www.wolfandraven.blog/?author=1"
	owner := models.Actor{
		ID: wordpressActor,
		PublicKey: models.PublicKey{
			Owner: wordpressActor,
		},
	}
	if err := verifySignatureActorBinding(
		wordpressActor+"#main-key",
		owner,
		wordpressActor,
	); err != nil {
		t.Fatalf("same-domain WordPress signature binding rejected: %v", err)
	}
	if err := verifySignatureActorBinding(
		"https://attacker.example/users/mallory#main-key",
		models.Actor{ID: "https://attacker.example/users/mallory"},
		wordpressActor,
	); err == nil {
		t.Fatal("cross-domain signature key was accepted")
	}
	if err := verifyActivityActorDocument(wordpressActor, models.Actor{ID: wordpressActor}); err != nil {
		t.Fatalf("same-domain actor document rejected: %v", err)
	}
	if err := verifyActivityActorDocument(wordpressActor, models.Actor{ID: "https://attacker.example/users/mallory"}); err == nil {
		t.Fatal("cross-domain actor document was accepted")
	}
}
