package api

import (
	"testing"

	"github.com/thystra/Activity-Relay/models"
)

func TestIsActorAbleToBeFollower(t *testing.T) {
	tests := []struct {
		name  string
		actor *models.Actor
		want  bool
	}{
		{
			name: "relay actor with legacy incomplete type",
			actor: &models.Actor{
				ID:   "https://example.social/relay",
				Type: "Person",
			},
			want: true,
		},
		{
			name: "relay actor with trailing slash",
			actor: &models.Actor{
				ID:   "https://example.social/relay/",
				Type: "Person",
			},
			want: true,
		},
		{
			name: "Friendica server actor with legacy incomplete type",
			actor: &models.Actor{
				ID:   "https://example.social/friendica",
				Type: "Person",
			},
			want: true,
		},
		{
			name: "Friendica server actor with trailing slash",
			actor: &models.Actor{
				ID:   "https://example.social/friendica/",
				Type: "Person",
			},
			want: true,
		},
		{
			name: "NodeBB Application actor",
			actor: &models.Actor{
				ID:   "https://activitypub.space/actor",
				Type: "Application",
			},
			want: true,
		},
		{
			name: "Service actor at implementation-defined path",
			actor: &models.Actor{
				ID:   "https://example.social/server-actor",
				Type: "Service",
			},
			want: true,
		},
		{
			name: "ordinary user actor",
			actor: &models.Actor{
				ID:   "https://example.social/users/alice",
				Type: "Person",
			},
			want: false,
		},
		{
			name: "missing host",
			actor: &models.Actor{
				ID:   "/actor",
				Type: "Application",
			},
			want: false,
		},
		{
			name:  "nil actor",
			actor: nil,
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := isActorAbleToBeFollower(test.actor)
			if got != test.want {
				t.Errorf(
					"isActorAbleToBeFollower(%#v) = %v; want %v",
					test.actor,
					got,
					test.want,
				)
			}
		})
	}
}
