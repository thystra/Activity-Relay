package api

import (
	"errors"
	"testing"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
	"github.com/thystra/Activity-Relay/models"
)

func TestDurableDirectoryEnabledTreatsGateDisableAndRemovalAsSuppression(t *testing.T) {
	config := directoryconfig.Config{
		SchedulerEnabled: false,
		Directories: []directoryclient.Directory{
			{Origin: "https://directory.example", Enabled: true},
		},
	}
	enabled, err := durableDirectoryEnabled(config, "https://directory.example")
	if err != nil || enabled {
		t.Fatalf("gate-disabled result = (%t, %v)", enabled, err)
	}

	config.SchedulerEnabled = true
	enabled, err = durableDirectoryEnabled(config, "https://removed.example")
	if err != nil || enabled {
		t.Fatalf("removed result = (%t, %v)", enabled, err)
	}

	_, err = durableDirectoryEnabled(config, "not an origin")
	if !errors.Is(err, directoryconfig.ErrConfiguration) {
		t.Fatalf("malformed origin error = %v", err)
	}
}

func TestParticipatingInstanceCountMatchesStatusSemantics(t *testing.T) {
	snapshot := models.RelayStateSnapshot{
		SubscribersAndFollowers: []models.Subscriber{
			{Domain: "z.example"},
			{Domain: "a.example"},
			{Domain: "Z.EXAMPLE."},
			{Domain: ""},
		},
		Publishers: []models.Publisher{
			{Domain: "publisher.example"},
			{Domain: "A.EXAMPLE"},
		},
	}

	if got, want := participatingInstanceCount(snapshot), 3; got != want {
		t.Fatalf("participating instance count = %d; want %d", got, want)
	}
}
