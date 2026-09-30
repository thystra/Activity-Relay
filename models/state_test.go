package models

import (
	"context"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()
	relayState.Load()
	snapshot := relayState.Snapshot()

	if snapshot.RelayConfig.PersonOnly != false {
		t.Fatalf("Expected PersonOnly to be false, but got %v", snapshot.RelayConfig.PersonOnly)
	}
	if snapshot.RelayConfig.ManuallyAccept != false {
		t.Fatalf("Expected ManuallyAccept to be false, but got %v", snapshot.RelayConfig.ManuallyAccept)
	}
}

func TestSetConfig(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()

	t.Run("Set PersonOnly to true", func(t *testing.T) {
		relayState.SetConfig(PersonOnly, true)
		<-ch
		if got := relayState.Snapshot().RelayConfig.PersonOnly; got != true {
			t.Fatalf("Expected PersonOnly to be true, but got %v", got)
		}
	})

	t.Run("Set ManuallyAccept to true", func(t *testing.T) {
		relayState.SetConfig(ManuallyAccept, true)
		<-ch
		if got := relayState.Snapshot().RelayConfig.ManuallyAccept; got != true {
			t.Fatalf("Expected ManuallyAccept to be true, but got %v", got)
		}
	})

	t.Run("Set PersonOnly to false", func(t *testing.T) {
		relayState.SetConfig(PersonOnly, false)
		<-ch
		if got := relayState.Snapshot().RelayConfig.PersonOnly; got != false {
			t.Fatalf("Expected PersonOnly to be false, but got %v", got)
		}
	})

	t.Run("Set ManuallyAccept to false", func(t *testing.T) {
		relayState.SetConfig(ManuallyAccept, false)
		<-ch
		if got := relayState.Snapshot().RelayConfig.ManuallyAccept; got != false {
			t.Fatalf("Expected ManuallyAccept to be false, but got %v", got)
		}
	})
}

func TestTreatSubscriptionNotify(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()

	t.Run("Add subscriber", func(t *testing.T) {
		relayState.AddSubscriber(Subscriber{
			Domain:   "example.com",
			InboxURL: "https://example.com/inbox",
		})
		<-ch

		valid := false
		for _, domain := range relayState.Snapshot().Subscribers {
			if domain.Domain == "example.com" && domain.InboxURL == "https://example.com/inbox" {
				valid = true
			}
		}
		if !valid {
			t.Fatalf("Expected subscriber 'example.com' with inbox 'https://example.com/inbox' to be present, but not found")
		}
	})

	t.Run("Delete subscriber", func(t *testing.T) {
		relayState.DelSubscriber("example.com")
		<-ch

		valid := true
		for _, domain := range relayState.Snapshot().Subscribers {
			if domain.Domain == "example.com" {
				valid = false
			}
		}
		if !valid {
			t.Fatalf("Expected subscriber 'example.com' to be deleted, but still found")
		}
	})
}

func TestSelectDomain(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()

	exampleSubscription := Subscriber{
		Domain:   "example.com",
		InboxURL: "https://example.com/inbox",
	}

	relayState.AddSubscriber(exampleSubscription)
	<-ch

	t.Run("Select existing subscriber", func(t *testing.T) {
		subscription := relayState.SelectSubscriber("example.com")
		if *subscription != exampleSubscription {
			t.Fatalf("Expected to select subscriber %+v, but got %+v", exampleSubscription, *subscription)
		}
	})

	t.Run("Select non-existent subscriber", func(t *testing.T) {
		subscription := relayState.SelectSubscriber("example.org")
		if subscription != nil {
			t.Fatalf("Expected nil for non-existent subscriber 'example.org', but got %+v", *subscription)
		}
	})
}

func TestBlockedDomain(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()

	t.Run("Set blocked domain to true", func(t *testing.T) {
		relayState.SetBlockedDomain("example.com", true)
		<-ch

		valid := false
		for _, domain := range relayState.Snapshot().BlockedDomains {
			if domain == "example.com" {
				valid = true
			}
		}
		if !valid {
			t.Fatalf("Expected blocked domain 'example.com' to be present, but not found")
		}
	})

	t.Run("Set blocked domain to false", func(t *testing.T) {
		relayState.SetBlockedDomain("example.com", false)
		<-ch

		valid := true
		for _, domain := range relayState.Snapshot().BlockedDomains {
			if domain == "example.com" {
				valid = false
			}
		}
		if !valid {
			t.Fatalf("Expected blocked domain 'example.com' to be removed, but still found")
		}
	})
}

func TestLimitedDomain(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()

	t.Run("Set limited domain to true", func(t *testing.T) {
		relayState.SetLimitedDomain("example.com", true)
		<-ch

		valid := false
		for _, domain := range relayState.Snapshot().LimitedDomains {
			if domain == "example.com" {
				valid = true
			}
		}
		if !valid {
			t.Fatalf("Expected limited domain 'example.com' to be present, but not found")
		}
	})

	t.Run("Set limited domain to false", func(t *testing.T) {
		relayState.SetLimitedDomain("example.com", false)
		<-ch

		valid := true
		for _, domain := range relayState.Snapshot().LimitedDomains {
			if domain == "example.com" {
				valid = false
			}
		}
		if !valid {
			t.Fatalf("Expected limited domain 'example.com' to be removed, but still found")
		}
	})
}

func TestLoadCompatibleSubscription(t *testing.T) {
	relayState.RedisClient.FlushAll(context.TODO()).Result()

	relayState.AddSubscriber(Subscriber{
		Domain:   "example.com",
		InboxURL: "https://example.com/inbox",
	})

	relayState.RedisClient.HDel(context.TODO(), "relay:subscription:example.com", "activity_id", "actor_id")
	relayState.Load()

	valid := false
	for _, domain := range relayState.Snapshot().Subscribers {
		if domain.Domain == "example.com" && domain.InboxURL == "https://example.com/inbox" {
			valid = true
		}
	}
	if !valid {
		t.Fatalf("Expected compatible subscriber 'example.com' with inbox 'https://example.com/inbox' to be present, but not found")
	}
}

func TestAddFollowerRejectsIncompleteRecord(t *testing.T) {
	ctx := context.Background()
	if err := relayState.RedisClient.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	follower := Follower{
		Domain:         "orphan.example",
		MutuallyFollow: false,
	}
	if err := relayState.AddFollowerChecked(follower); err == nil {
		t.Fatal("expected incomplete follower to be rejected")
	}
	exists, err := relayState.RedisClient.Exists(ctx, "relay:follower:orphan.example").Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatalf("incomplete follower key exists after rejection: %d", exists)
	}
}

func TestUpdateFollowerStatusDoesNotCreateMissingFollower(t *testing.T) {
	ctx := context.Background()
	if err := relayState.RedisClient.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}

	if err := relayState.UpdateFollowerStatusChecked("orphan.example", false); err == nil {
		t.Fatal("expected missing follower status update to fail")
	}
	exists, err := relayState.RedisClient.Exists(ctx, "relay:follower:orphan.example").Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatalf("stale status update recreated follower key: %d", exists)
	}
}

func TestUpdateFollowerStatusRemovesIncompleteFollower(t *testing.T) {
	ctx := context.Background()
	if err := relayState.RedisClient.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	key := "relay:follower:orphan.example"
	if err := relayState.RedisClient.HSet(ctx, key, "mutually_follow", "0").Err(); err != nil {
		t.Fatal(err)
	}

	if err := relayState.UpdateFollowerStatusChecked("orphan.example", true); err == nil {
		t.Fatal("expected incomplete follower status update to fail")
	}
	<-ch
	exists, err := relayState.RedisClient.Exists(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Fatalf("incomplete follower key was not removed: %d", exists)
	}
}

func TestLoadSkipsIncompleteFollowerAndKeepsHealthyFollower(t *testing.T) {
	ctx := context.Background()
	if err := relayState.RedisClient.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := relayState.RedisClient.HSet(ctx, "relay:follower:orphan.example", map[string]interface{}{
		"mutually_follow": "0",
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := relayState.RedisClient.HSet(ctx, "relay:follower:friendica.example", map[string]interface{}{
		"inbox_url":       "https://friendica.example/friendica/inbox",
		"activity_id":     "https://friendica.example/activity/follow-test",
		"actor_id":        "https://friendica.example/friendica",
		"mutually_follow": "0",
	}).Err(); err != nil {
		t.Fatal(err)
	}

	if err := relayState.Load(); err != nil {
		t.Fatal(err)
	}
	snapshot := relayState.Snapshot()
	if len(snapshot.Followers) != 1 {
		t.Fatalf("followers = %d; want 1 healthy follower", len(snapshot.Followers))
	}
	if snapshot.Followers[0].Domain != "friendica.example" {
		t.Fatalf("loaded follower = %q; want friendica.example", snapshot.Followers[0].Domain)
	}
	if len(snapshot.SubscribersAndFollowers) != 1 {
		t.Fatalf("fan-out targets = %d; want 1 healthy target", len(snapshot.SubscribersAndFollowers))
	}
	if snapshot.SubscribersAndFollowers[0].InboxURL != "https://friendica.example/friendica/inbox" {
		t.Fatalf("fan-out inbox = %q; want Friendica inbox", snapshot.SubscribersAndFollowers[0].InboxURL)
	}
}

func TestUpdateFollowerStatusUpdatesCompleteFollower(t *testing.T) {
	ctx := context.Background()
	if err := relayState.RedisClient.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	follower := Follower{
		Domain:         "friendica.example",
		InboxURL:       "https://friendica.example/friendica/inbox",
		ActivityID:     "https://friendica.example/activity/follow-test",
		ActorID:        "https://friendica.example/friendica",
		MutuallyFollow: false,
	}
	if err := relayState.AddFollowerChecked(follower); err != nil {
		t.Fatal(err)
	}
	<-ch
	if err := relayState.UpdateFollowerStatusChecked(follower.Domain, true); err != nil {
		t.Fatal(err)
	}
	<-ch
	value, err := relayState.RedisClient.HGet(ctx, "relay:follower:"+follower.Domain, "mutually_follow").Result()
	if err != nil {
		t.Fatal(err)
	}
	if value != "1" {
		t.Fatalf("mutually_follow = %q; want 1", value)
	}
}
