package api

import (
	"testing"

	"github.com/thystra/Activity-Relay/models"
)

func TestActorAbleToFollowRelayActor(t *testing.T) {
	tests := []struct {
		name    string
		actor   *models.Actor
		allowed bool
	}{
		{
			name: "NodeBB Application actor at slash actor",
			actor: &models.Actor{
				ID:   "https://activitypub.space/actor",
				Type: "Application",
			},
			allowed: true,
		},
		{
			name: "Service actor at implementation-defined path",
			actor: &models.Actor{
				ID:   "https://nodebb.example/server",
				Type: "Service",
			},
			allowed: true,
		},
		{
			name: "ordinary Person actor at slash actor",
			actor: &models.Actor{
				ID:   "https://social.example/actor",
				Type: "Person",
			},
			allowed: false,
		},
		{
			name: "legacy LitePub relay path",
			actor: &models.Actor{
				ID:   "https://pleroma.example/relay",
				Type: "Person",
			},
			allowed: true,
		},
		{
			name: "legacy Friendica path with trailing slash",
			actor: &models.Actor{
				ID:   "https://friendica.example/friendica/",
				Type: "Person",
			},
			allowed: true,
		},
		{
			name:    "missing actor",
			actor:   nil,
			allowed: false,
		},
		{
			name: "actor ID without a host",
			actor: &models.Actor{
				ID:   "/actor",
				Type: "Application",
			},
			allowed: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := isActorAbleToBeFollower(test.actor)
			if actual != test.allowed {
				t.Fatalf(
					"Expected allowed=%t, got %t for actor %#v",
					test.allowed,
					actual,
					test.actor,
				)
			}
		})
	}
}
