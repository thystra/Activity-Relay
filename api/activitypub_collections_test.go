package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/thystra/Activity-Relay/models"
)

func TestRelayActorAdvertisesStandardCollections(t *testing.T) {
	base := GlobalConfig.ServerHostname().String()
	if RelayActor.Inbox != base+"/inbox" {
		t.Fatalf("unexpected inbox: %q", RelayActor.Inbox)
	}
	if RelayActor.Outbox != base+"/actor/outbox" {
		t.Fatalf("unexpected outbox: %q", RelayActor.Outbox)
	}
	if RelayActor.FollowersURL != base+"/actor/followers" {
		t.Fatalf("unexpected followers collection: %q", RelayActor.FollowersURL)
	}
	if RelayActor.FollowingURL != base+"/actor/following" {
		t.Fatalf("unexpected following collection: %q", RelayActor.FollowingURL)
	}
	if RelayActor.Endpoints == nil || RelayActor.Endpoints.SharedInbox != RelayActor.Inbox {
		t.Fatalf("sharedInbox must match the relay inbox: %#v", RelayActor.Endpoints)
	}
	if RelayActor.Followers() != RelayActor.FollowersURL {
		t.Fatalf("Followers() did not use the advertised collection")
	}
}

func TestRelayActorResponseIncludesStandardCollections(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/actor", nil)
	response := httptest.NewRecorder()
	handleRelayActor(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != activityPubContentType {
		t.Fatalf("unexpected content type: %q", got)
	}

	var actor models.Actor
	if err := json.Unmarshal(response.Body.Bytes(), &actor); err != nil {
		t.Fatalf("invalid actor JSON: %v", err)
	}
	if actor.Outbox == "" || actor.FollowersURL == "" || actor.FollowingURL == "" {
		t.Fatalf("actor omitted standard collections: %#v", actor)
	}
	if actor.Endpoints == nil || actor.Endpoints.SharedInbox == "" {
		t.Fatalf("actor omitted endpoints.sharedInbox: %#v", actor.Endpoints)
	}
}

func TestActivityPubCollectionEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	handlersRegister(mux)

	tests := []struct {
		path string
		id   string
	}{
		{"/inbox", RelayActor.Inbox},
		{"/actor/outbox", RelayActor.Outbox},
		{"/actor/followers", RelayActor.FollowersURL},
		{"/actor/following", RelayActor.FollowingURL},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", response.Code)
			}
			if got := response.Header().Get("Content-Type"); got != activityPubContentType {
				t.Fatalf("unexpected content type: %q", got)
			}

			var collection emptyOrderedCollection
			if err := json.Unmarshal(response.Body.Bytes(), &collection); err != nil {
				t.Fatalf("invalid collection JSON: %v", err)
			}
			if collection.ID != test.id || collection.Type != "OrderedCollection" {
				t.Fatalf("unexpected collection: %#v", collection)
			}
			if collection.TotalItems != 0 || collection.OrderedItems == nil || len(collection.OrderedItems) != 0 {
				t.Fatalf("collection must be privacy-filtered and empty: %#v", collection)
			}
		})
	}
}

func TestActivityPubCollectionHeadAndMethodHandling(t *testing.T) {
	mux := http.NewServeMux()
	handlersRegister(mux)

	head := httptest.NewRequest(http.MethodHead, "/actor/outbox", nil)
	headResponse := httptest.NewRecorder()
	mux.ServeHTTP(headResponse, head)
	if headResponse.Code != http.StatusOK {
		t.Fatalf("expected HEAD 200, got %d", headResponse.Code)
	}
	if headResponse.Body.Len() != 0 {
		t.Fatalf("HEAD returned a response body")
	}

	post := httptest.NewRequest(http.MethodPost, "/actor/outbox", nil)
	postResponse := httptest.NewRecorder()
	mux.ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected POST 405, got %d", postResponse.Code)
	}
	if got := postResponse.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("unexpected Allow header: %q", got)
	}
}
