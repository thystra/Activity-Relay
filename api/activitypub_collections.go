package api

import (
	"encoding/json"
	"net/http"

	"github.com/sirupsen/logrus"
)

const activityPubContentType = "application/activity+json"

type emptyOrderedCollection struct {
	Context      string        `json:"@context"`
	ID           string        `json:"id"`
	Type         string        `json:"type"`
	TotalItems   int           `json:"totalItems"`
	OrderedItems []interface{} `json:"orderedItems"`
}

func writeActivityPubJSON(writer http.ResponseWriter, request *http.Request, value interface{}) {
	payload, err := json.Marshal(value)
	if err != nil {
		logrus.Error("Failed to marshal ActivityPub response: ", err.Error())
		http.Error(writer, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("Content-Type", activityPubContentType)
	writer.WriteHeader(http.StatusOK)
	if request.Method == http.MethodHead {
		return
	}
	_, _ = writer.Write(payload)
}

func handleReadOnlyOrderedCollection(writer http.ResponseWriter, request *http.Request, id string, allow string) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		writeActivityPubJSON(writer, request, emptyOrderedCollection{
			Context:      "https://www.w3.org/ns/activitystreams",
			ID:           id,
			Type:         "OrderedCollection",
			TotalItems:   0,
			OrderedItems: make([]interface{}, 0),
		})
	default:
		writer.Header().Set("Allow", allow)
		writer.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleRelayOutbox(writer http.ResponseWriter, request *http.Request) {
	handleReadOnlyOrderedCollection(writer, request, RelayActor.Outbox, "GET, HEAD")
}

func handleRelayFollowers(writer http.ResponseWriter, request *http.Request) {
	handleReadOnlyOrderedCollection(writer, request, RelayActor.FollowersURL, "GET, HEAD")
}

func handleRelayFollowing(writer http.ResponseWriter, request *http.Request) {
	handleReadOnlyOrderedCollection(writer, request, RelayActor.FollowingURL, "GET, HEAD")
}
