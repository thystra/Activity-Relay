package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/models"
)

func handleWebfinger(writer http.ResponseWriter, request *http.Request) {
	queriedResource := request.URL.Query()["resource"]
	if request.Method != "GET" || len(queriedResource) == 0 {
		writer.WriteHeader(400)
		writer.Write(nil)
	} else {
		queriedSubject := queriedResource[0]
		for _, webfingerResource := range WebfingerResources {
			if queriedSubject == webfingerResource.Subject {
				webfinger, err := json.Marshal(&webfingerResource)
				if err != nil {
					logrus.Fatal("Failed to marshal webfinger resource : ", err.Error())
					writer.WriteHeader(500)
					writer.Write(nil)
					return
				}
				writer.Header().Add("Content-Type", "application/json")
				writer.WriteHeader(200)
				writer.Write(webfinger)
				return
			}
		}
		writer.WriteHeader(404)
		writer.Write(nil)
	}
}

func handleNodeinfoLink(writer http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" {
		writer.WriteHeader(400)
		writer.Write(nil)
	} else {
		nodeinfoLinks, err := json.Marshal(&Nodeinfo.NodeinfoLinks)
		if err != nil {
			logrus.Fatal("Failed to marshal nodeinfo links : ", err.Error())
			writer.WriteHeader(500)
			writer.Write(nil)
			return
		}
		writer.Header().Add("Content-Type", "application/json")
		writer.WriteHeader(200)
		writer.Write(nodeinfoLinks)
	}
}

func handleNodeinfo(writer http.ResponseWriter, request *http.Request) {
	if request.Method != "GET" {
		writer.WriteHeader(400)
		writer.Write(nil)
	} else {
		userTotal := len(RelayState.Snapshot().Subscribers)
		Nodeinfo.Nodeinfo.Usage.Users.Total = userTotal
		Nodeinfo.Nodeinfo.Usage.Users.ActiveMonth = userTotal
		Nodeinfo.Nodeinfo.Usage.Users.ActiveHalfyear = userTotal
		nodeinfo, err := json.Marshal(&Nodeinfo.Nodeinfo)
		if err != nil {
			logrus.Fatal("Failed to marshal nodeinfo : ", err.Error())
			writer.WriteHeader(500)
			writer.Write(nil)
			return
		}
		writer.Header().Add("Content-Type", "application/json")
		writer.WriteHeader(200)
		writer.Write(nodeinfo)
	}
}

func handleRelayActor(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		writeActivityPubJSON(writer, request, &RelayActor)
	default:
		writer.Header().Set("Allow", "GET, HEAD")
		writer.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func boundedLogValue(value string, maximum int) string {
	if maximum < 1 || len(value) <= maximum {
		return value
	}
	return value[:maximum] + "..."
}

func logInboxDecodeFailure(request *http.Request, err error) {
	logrus.WithFields(logrus.Fields{
		"error":       boundedLogValue(err.Error(), 512),
		"method":      request.Method,
		"path":        request.URL.Path,
		"remote_addr": boundedLogValue(request.RemoteAddr, 256),
		"user_agent":  boundedLogValue(request.UserAgent(), 256),
	}).Warn("Rejected inbox activity")
}

func shouldFanOutPublicAnnounce(activity *models.Activity) bool {
	if activity == nil || activity.Type != "Announce" {
		return false
	}
	if _, ok := activity.Object.(map[string]interface{}); !ok {
		return false
	}
	objectID, err := activity.UnwrapInnerObjectId()
	if err != nil {
		return false
	}
	actorURL, err := url.Parse(activity.Actor)
	if err != nil {
		return false
	}
	objectURL, err := url.Parse(objectID)
	if err != nil {
		return false
	}
	actorDomain := normalizedActorDomain(actorURL)
	return actorDomain != "" && actorDomain == normalizedActorDomain(objectURL)
}

func executePublicAnnounce(activity *models.Activity, actor *models.Actor, body []byte) error {
	if shouldFanOutPublicAnnounce(activity) {
		return executeRelayActivity(activity, actor, body)
	}
	return recordPublisherActivity(activity, actor)
}
func handleInbox(writer http.ResponseWriter, request *http.Request, activityDecoder func(*http.Request) (*models.Activity, *models.Actor, []byte, error)) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
		handleReadOnlyOrderedCollection(writer, request, RelayActor.Inbox, "GET, HEAD, POST")
	case http.MethodPost:
		activity, actor, body, err := activityDecoder(request)
		if err != nil {
			logInboxDecodeFailure(request, err)
			writer.WriteHeader(http.StatusBadRequest)
			writer.Write(nil)
		} else {
			actorID, _ := url.Parse(activity.Actor)
			switch {
			case contains(activity.To, "https://www.w3.org/ns/activitystreams#Public"), contains(activity.Cc, "https://www.w3.org/ns/activitystreams#Public"):
				// Mastodon Traditional Style (Activity Transfer)
				switch activity.Type {
				case "Create", "Update", "Delete", "Move":
					err = executeRelayActivity(activity, actor, body)
					if err != nil {
						writer.WriteHeader(401)
						writer.Write([]byte(err.Error()))

						return
					}
					writer.WriteHeader(202)
					writer.Write(nil)
				case "Announce":
					err = executePublicAnnounce(activity, actor, body)
					if err != nil {
						writer.WriteHeader(401)
						writer.Write([]byte(err.Error()))
						return
					}
					writer.WriteHeader(202)
					writer.Write(nil)
				default:
					writer.WriteHeader(202)
					writer.Write(nil)
				}
			case contains(activity.To, RelayActor.ID), contains(activity.Cc, RelayActor.ID):
				// LitePub Relay Style
				fallthrough
			case isToMyFollower(activity.To), isToMyFollower(activity.Cc):
				// LitePub Relay Style
				switch activity.Type {
				case "Follow":
					err = executeFollowing(activity, actor)
					if err != nil {
						executeRejectRequest(activity, actor, err)
					}
					writer.WriteHeader(202)
					writer.Write(nil)
				case "Undo":
					innerActivity, err := activity.UnwrapInnerActivity()
					if err != nil {
						writer.WriteHeader(202)
						writer.Write(nil)

						return
					}
					switch innerActivity.Type {
					case "Follow":
						err = executeUnfollowing(innerActivity, actor)
						if err != nil {
							executeRejectRequest(activity, actor, err)
						}
						writer.WriteHeader(202)
						writer.Write(nil)
					default:
						writer.WriteHeader(202)
						writer.Write(nil)
					}
				case "Accept":
					innerActivity, err := activity.UnwrapInnerActivity()
					if err != nil {
						writer.WriteHeader(202)
						writer.Write(nil)

						return
					}
					switch innerActivity.Type {
					case "Follow":
						finalizeMutuallyFollow(innerActivity, actor, activity.Type)
						writer.WriteHeader(202)
						writer.Write(nil)
					default:
						writer.WriteHeader(202)
						writer.Write(nil)
					}
				case "Reject":
					innerActivity, err := activity.UnwrapInnerActivity()
					if err != nil {
						writer.WriteHeader(202)
						writer.Write(nil)

						return
					}
					switch innerActivity.Type {
					case "Follow":
						finalizeMutuallyFollow(innerActivity, actor, activity.Type)
						writer.WriteHeader(202)
						writer.Write(nil)
					default:
						writer.WriteHeader(202)
						writer.Write(nil)
					}
				case "Announce":
					if !isActorSubscribersOrFollowers(actorID) {
						err = errors.New("to use the relay service, please follow in advance")
						writer.WriteHeader(401)
						writer.Write([]byte(err.Error()))

						return
					}
					switch innerObject := activity.Object.(type) {
					case string:
						origActivity, origActor, err := fetchOriginalActivityFromURL(innerObject)
						if err != nil {
							logrus.Debug("Failed Announce Activity : ", activity.Actor)
							writer.WriteHeader(400)
							writer.Write([]byte(err.Error()))

							return
						}
						executeAnnounceActivity(origActivity, origActor)
					default:
						logrus.Debug("Skipped Announce Activity : ", activity.Actor)
					}
					writer.WriteHeader(202)
					writer.Write(nil)
				default:
					writer.WriteHeader(202)
					writer.Write(nil)
				}
			default:
				// Follow, Unfollow Only
				switch activity.Type {
				case "Follow":
					err = executeFollowing(activity, actor)
					if err != nil {
						executeRejectRequest(activity, actor, err)
					}
					writer.WriteHeader(202)
					writer.Write(nil)
				case "Undo":
					innerActivity, err := activity.UnwrapInnerActivity()
					if err != nil {
						writer.WriteHeader(202)
						writer.Write(nil)

						return
					}
					switch innerActivity.Type {
					case "Follow":
						err = executeUnfollowing(innerActivity, actor)
						if err != nil {
							executeRejectRequest(activity, actor, err)
						}
						writer.WriteHeader(202)
						writer.Write(nil)
					default:
						writer.WriteHeader(202)
						writer.Write(nil)
					}
				default:
					writer.WriteHeader(202)
					writer.Write(nil)
				}
			}
		}
	default:
		writer.Header().Set("Allow", "GET, HEAD, POST")
		writer.WriteHeader(http.StatusMethodNotAllowed)
	}
}
