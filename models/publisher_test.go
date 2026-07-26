package models

import (
	"context"
	"testing"
)

func TestRecordPublisherActivity(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()
	if err := relayState.Load(); err != nil {
		t.Fatal(err)
	}

	firstSeen := "2026-07-26T17:30:13Z"
	if err := relayState.RecordPublisherActivity(Publisher{
		Domain:           "WWW.Example.COM.",
		ActorID:          "https://www.example.com/?author=1",
		InboxURL:         "https://www.example.com/wp-json/activitypub/1.0/inbox",
		LastSeen:         firstSeen,
		LastActivityID:   "https://www.example.com/activities/1",
		LastActivityType: "Create",
	}); err != nil {
		t.Fatal(err)
	}

	publisher := relayState.SelectPublisher("www.example.com")
	if publisher == nil {
		t.Fatal("publisher was not recorded")
	}
	if publisher.Domain != "www.example.com" {
		t.Fatalf("domain = %q; want www.example.com", publisher.Domain)
	}
	if publisher.FirstSeen != firstSeen || publisher.LastSeen != firstSeen {
		t.Fatalf("unexpected first/last seen: %+v", publisher)
	}
	if publisher.ActivityCount != 1 {
		t.Fatalf("activity count = %d; want 1", publisher.ActivityCount)
	}

	secondSeen := "2026-07-26T17:31:13Z"
	if err := relayState.RecordPublisherActivity(Publisher{
		Domain:           "www.example.com",
		ActorID:          "https://www.example.com/?author=1",
		InboxURL:         "https://www.example.com/wp-json/activitypub/1.0/inbox",
		LastSeen:         secondSeen,
		LastActivityID:   "https://www.example.com/activities/2",
		LastActivityType: "Update",
	}); err != nil {
		t.Fatal(err)
	}

	publisher = relayState.SelectPublisher("www.example.com")
	if publisher.FirstSeen != firstSeen {
		t.Fatalf("first seen changed to %q; want %q", publisher.FirstSeen, firstSeen)
	}
	if publisher.LastSeen != secondSeen || publisher.LastActivityType != "Update" {
		t.Fatalf("publisher was not updated: %+v", publisher)
	}
	if publisher.ActivityCount != 2 {
		t.Fatalf("activity count = %d; want 2", publisher.ActivityCount)
	}

	reloaded := NewState(relayState.RedisClient, false)
	publisher = reloaded.SelectPublisher("www.example.com")
	if publisher == nil || publisher.ActivityCount != 2 || publisher.FirstSeen != firstSeen {
		t.Fatalf("publisher did not survive state reload: %+v", publisher)
	}
}
