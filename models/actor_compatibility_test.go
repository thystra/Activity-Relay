package models

import (
	"net/url"
	"testing"
)

// friendica202605RecognizesRelayActor mirrors the explicit relay-actor rule
// used by Friendica 2026.05: an Application actor named relay at /actor or
// /relay is recognized even when it publishes following/followers collections.
func friendica202605RecognizesRelayActor(actor Actor) bool {
	if actor.Type != "Application" || actor.PreferredUsername != "relay" {
		return false
	}

	actorURL, err := url.Parse(actor.ID)
	if err != nil {
		return false
	}

	return actorURL.Path == "/actor" || actorURL.Path == "/relay"
}

func TestRelayActorUsesApplicationTypeForFriendicaDiscovery(t *testing.T) {
	actor := NewActivityPubActorFromRelayConfig(globalConfig)

	if actor.Type != "Application" {
		t.Fatalf("relay actor type = %q; want Application", actor.Type)
	}
	if !friendica202605RecognizesRelayActor(actor) {
		t.Fatal("Friendica 2026.05 relay discovery rule rejects generated actor")
	}

	serviceActor := actor
	serviceActor.Type = "Service"
	if friendica202605RecognizesRelayActor(serviceActor) {
		t.Fatal("compatibility fixture unexpectedly accepts Service actor")
	}
}

func TestRelayActorApplicationProfilePreservesIdentityAndCollections(t *testing.T) {
	actor := NewActivityPubActorFromRelayConfig(globalConfig)
	base := globalConfig.domain.String()

	expected := map[string]struct {
		got  string
		want string
	}{
		"actor ID":         {actor.ID, base + "/actor"},
		"inbox":            {actor.Inbox, base + "/inbox"},
		"outbox":           {actor.Outbox, base + "/actor/outbox"},
		"following":        {actor.FollowingURL, base + "/actor/following"},
		"followers":        {actor.FollowersURL, base + "/actor/followers"},
		"public key ID":    {actor.PublicKey.ID, base + "/actor#main-key"},
		"public key owner": {actor.PublicKey.Owner, base + "/actor"},
	}

	if actor.PreferredUsername != "relay" {
		t.Fatalf(
			"preferredUsername = %q; want relay",
			actor.PreferredUsername,
		)
	}
	if actor.Endpoints == nil {
		t.Fatal("relay actor endpoints are nil")
	}
	expected["shared inbox"] = struct {
		got  string
		want string
	}{actor.Endpoints.SharedInbox, base + "/inbox"}

	for name, values := range expected {
		if values.got != values.want {
			t.Errorf("%s = %q; want %q", name, values.got, values.want)
		}
	}
	if actor.PublicKey.PublicKeyPem == "" {
		t.Fatal("relay actor public key PEM is empty")
	}
}
