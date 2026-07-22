package api

import (
	"net/url"
	"testing"
)

func TestIsActorAbleToBeFollower(t *testing.T) {
	tests := []struct {
		name    string
		actorID string
		want    bool
	}{
		{
			name:    "relay actor",
			actorID: "https://example.social/relay",
			want:    true,
		},
		{
			name:    "relay actor with trailing slash",
			actorID: "https://example.social/relay/",
			want:    true,
		},
		{
			name:    "Friendica server actor",
			actorID: "https://example.social/friendica",
			want:    true,
		},
		{
			name:    "Friendica server actor with trailing slash",
			actorID: "https://example.social/friendica/",
			want:    true,
		},
		{
			name:    "ordinary user actor",
			actorID: "https://example.social/users/alice",
			want:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actorID, err := url.Parse(test.actorID)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", test.actorID, err)
			}

			got := isActorAbleToBeFollower(actorID)
			if got != test.want {
				t.Errorf(
					"isActorAbleToBeFollower(%q) = %v; want %v",
					test.actorID,
					got,
					test.want,
				)
			}
		})
	}
}
