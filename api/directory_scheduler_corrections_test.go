package api

import (
	"errors"
	"testing"

	"github.com/thystra/Activity-Relay/internal/directoryclient"
	"github.com/thystra/Activity-Relay/internal/directoryconfig"
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
