package api

import (
	"context"
	"testing"

	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

func TestRelayTaskPersistsWireProfile(t *testing.T) {
	signature := relayTaskWithProfile(
		"https://receiver.example/inbox",
		"activity-storage-id",
		relayhttpsig.ProfileRFC9421,
	)
	if len(signature.Args) != 3 {
		t.Fatalf("task args = %d; want 3", len(signature.Args))
	}
	if got := signature.Args[2].Value; got != "rfc9421" {
		t.Fatalf("signature profile arg = %#v", got)
	}
}

func TestPlannedDeliveryProfileUsesConfiguredSigner(t *testing.T) {
	if RemoteRequestSigner == nil {
		t.Fatal("API outbound signer is nil")
	}
	plan, err := RemoteRequestSigner.Plan(
		context.Background(),
		relayhttpsig.DestinationScopeDelivery,
		"https://receiver.example/inbox",
	)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := plannedDeliveryProfile(
		"https://receiver.example/inbox",
	)
	if err != nil {
		t.Fatal(err)
	}
	if profile != plan.Primary {
		t.Fatalf(
			"planned delivery profile = %q; want %q",
			profile,
			plan.Primary,
		)
	}
}
