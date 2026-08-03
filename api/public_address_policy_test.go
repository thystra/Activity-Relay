// File: api/public_address_policy_test.go

package api

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/thystra/Activity-Relay/models"
)

func TestShouldDistributeByPublicAddress(t *testing.T) {
	public := fepAE0CPublic
	followers := "https://publisher.example/actor/followers"

	tests := []struct {
		name     string
		policy   models.PublicAddressDistributionPolicy
		to       []string
		cc       []string
		expected bool
	}{
		{
			name:     "explicit primary Public",
			policy:   models.PublicAddressExplicitPublicOnly,
			to:       []string{public},
			expected: true,
		},
		{
			name:     "explicit excludes cc-only Public",
			policy:   models.PublicAddressExplicitPublicOnly,
			to:       []string{followers},
			cc:       []string{public},
			expected: false,
		},
		{
			name:     "compatibility includes cc-only Public",
			policy:   models.PublicAddressPublicAndUnlisted,
			to:       []string{followers},
			cc:       []string{public},
			expected: true,
		},
		{
			name:     "followers-only is not public fan-out",
			policy:   models.PublicAddressPublicAndUnlisted,
			to:       []string{followers},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			activity := &models.Activity{
				Type: "Create",
				To:   test.to,
				Cc:   test.cc,
			}
			got := shouldDistributeByPublicAddress(
				activity,
				test.policy,
			)
			if got != test.expected {
				t.Fatalf(
					"shouldDistributeByPublicAddress() = %t; want %t",
					got,
					test.expected,
				)
			}
		})
	}
}

func TestExplicitPublicOnlyRecordsCCOnlyPublisherWithoutFanout(
	t *testing.T,
) {
	const receiverDomain = "policy-receiver.example"

	activity, _ := fixtureActivity(
		t,
		"traditional-create-public-cc",
	)
	actor := actorForFixtureActivity(t, activity, "Person")

	actorURL, err := url.Parse(actor.ID)
	if err != nil {
		t.Fatalf("parse fixture actor ID: %v", err)
	}
	sourceDomain := normalizedActorDomain(actorURL)
	if sourceDomain == "" {
		t.Fatalf("fixture actor has no normalized domain: %q", actor.ID)
	}

	resetFEPAE0CState(t, sourceDomain, receiverDomain)
	addFEPTraditionalReceiver(t, receiverDomain)
	t.Cleanup(func() {
		resetFEPAE0CState(t, sourceDomain, receiverDomain)
	})

	if !isPublicOnlyInCCExcluded(
		activity,
		models.PublicAddressExplicitPublicOnly,
	) {
		t.Fatal("cc-only Public activity was not classified as excluded")
	}

	if err := executePolicyExcludedPublicActivity(
		activity,
		actor,
		models.PublicAddressExplicitPublicOnly,
	); err != nil {
		t.Fatalf("execute policy-excluded activity: %v", err)
	}

	time.Sleep(250 * time.Millisecond)
	keys, err := models.ScanKeys(
		context.Background(),
		RelayState.RedisClient,
		"relay:activity:*",
	)
	if err != nil {
		t.Fatalf("scan relay activities: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("policy-excluded activity created relay work: %v", keys)
	}

	publisher := RelayState.SelectPublisher(sourceDomain)
	if publisher == nil {
		t.Fatalf(
			"policy-excluded source %q was not recorded as publisher",
			sourceDomain,
		)
	}
	if publisher.LastActivityType != "Create" {
		t.Fatalf(
			"publisher activity type = %q; want Create",
			publisher.LastActivityType,
		)
	}
}

func TestExplicitPolicyDoesNotClassifyPrimaryPublicAsExcluded(t *testing.T) {
	activity, _ := fixtureActivity(
		t,
		"traditional-create-public-to",
	)

	if isPublicOnlyInCCExcluded(
		activity,
		models.PublicAddressExplicitPublicOnly,
	) {
		t.Fatal("primary Public activity was classified as cc-only exclusion")
	}
}

// EOF: api/public_address_policy_test.go
