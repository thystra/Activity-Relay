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
