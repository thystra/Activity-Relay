package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/models"
)

func nodeBBPublicAnnounce(object interface{}) *models.Activity {
	return &models.Activity{
		Context: []string{"https://www.w3.org/ns/activitystreams"},
		ID:      "https://nodebb.example/post/3#activity/announce/cid/5",
		Actor:   "https://nodebb.example/category/5",
		Type:    "Announce",
		Object:  object,
		To:      []string{"https://nodebb.example/category/5/followers"},
		Cc:      []string{"https://www.w3.org/ns/activitystreams#Public"},
	}
}

func TestShouldFanOutPublicAnnounce(t *testing.T) {
	tests := []struct {
		name     string
		activity *models.Activity
		want     bool
	}{
		{
			name: "NodeBB embedded same-domain object",
			activity: nodeBBPublicAnnounce(map[string]interface{}{
				"id":   "https://nodebb.example/post/3",
				"type": "Article",
			}),
			want: true,
		},
		{
			name:     "relay-style string object",
			activity: nodeBBPublicAnnounce("https://origin.example/objects/1"),
			want:     false,
		},
		{
			name: "embedded cross-domain object",
			activity: nodeBBPublicAnnounce(map[string]interface{}{
				"id":   "https://origin.example/objects/1",
				"type": "Note",
			}),
			want: false,
		},
		{
			name: "embedded object without ID",
			activity: nodeBBPublicAnnounce(map[string]interface{}{
				"type": "Article",
			}),
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldFanOutPublicAnnounce(test.activity); got != test.want {
				t.Fatalf("shouldFanOutPublicAnnounce() = %v; want %v", got, test.want)
			}
		})
	}
}

func removeRelayActivityTestKeys(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	keys, err := models.ScanKeys(ctx, RelayState.RedisClient, "relay:activity:*")
	if err != nil {
		t.Fatalf("scan relay activity keys: %v", err)
	}
	if len(keys) > 0 {
		if err := RelayState.RedisClient.Del(ctx, keys...).Err(); err != nil {
			t.Fatalf("delete relay activity keys: %v", err)
		}
	}
	if err := RelayState.RedisClient.Del(ctx, "relay", queueReservationKey).Err(); err != nil {
		t.Fatalf("clear relay queue: %v", err)
	}
}

func TestExecutePublicAnnounceFansOutNodeBBEmbeddedObject(t *testing.T) {
	const (
		sourceDomain      = "nodebb.example"
		traditionalDomain = "traditional.example"
		followerDomain    = "follower.example"
		objectID          = "https://nodebb.example/post/rc6-5-regression"
	)

	ctx := context.Background()
	removeRelayActivityTestKeys(t)
	RelayState.RedisClient.Del(ctx, "relay:publisher:"+sourceDomain)
	RelayState.AddSubscriber(models.Subscriber{
		Domain:   traditionalDomain,
		InboxURL: "https://traditional.example/inbox",
	})
	RelayState.AddFollower(models.Follower{
		Domain:         followerDomain,
		InboxURL:       "https://follower.example/inbox",
		ActorID:        "https://follower.example/actor",
		MutuallyFollow: true,
	})
	t.Cleanup(func() {
		RelayState.DelSubscriber(traditionalDomain)
		RelayState.DelFollower(followerDomain)
		RelayState.RedisClient.Del(ctx, "relay:publisher:"+sourceDomain)
		removeRelayActivityTestKeys(t)
	})

	activity := nodeBBPublicAnnounce(map[string]interface{}{
		"id":   objectID,
		"type": "Article",
	})
	actor := &models.Actor{
		ID:    activity.Actor,
		Type:  "Group",
		Inbox: "https://nodebb.example/category/5/inbox",
		Endpoints: &models.Endpoints{
			SharedInbox: "https://nodebb.example/inbox",
		},
	}
	body, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal NodeBB Announce: %v", err)
	}
	if err := executePublicAnnounce(activity, actor, body); err != nil {
		t.Fatalf("executePublicAnnounce returned an error: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	foundOriginal := false
	foundRelayAnnounce := false
	for time.Now().Before(deadline) {
		keys, err := models.ScanKeys(ctx, RelayState.RedisClient, "relay:activity:*")
		if err != nil {
			t.Fatalf("scan relay activities: %v", err)
		}
		for _, key := range keys {
			storedBody, err := RelayState.RedisClient.HGet(ctx, key, "body").Bytes()
			if err != nil {
				continue
			}
			if bytes.Equal(storedBody, body) {
				foundOriginal = true
				continue
			}
			var relayed models.Activity
			if err := json.Unmarshal(storedBody, &relayed); err != nil {
				continue
			}
			if relayed.Type == "Announce" &&
				relayed.Actor == RelayActor.ID &&
				relayed.Object == objectID &&
				contains(relayed.To, RelayActor.Followers()) {
				foundRelayAnnounce = true
			}
		}
		if foundOriginal && foundRelayAnnounce {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if !foundOriginal {
		t.Fatal("original NodeBB Announce was not queued for traditional subscribers")
	}
	if !foundRelayAnnounce {
		t.Fatal("relay-signed Announce was not queued for follower-style subscribers")
	}
	publisher := RelayState.SelectPublisher(sourceDomain)
	if publisher == nil {
		t.Fatal("NodeBB source domain was not recorded as a publisher")
	}
	if publisher.LastActivityType != "Announce" {
		t.Fatalf("publisher activity type = %q; want Announce", publisher.LastActivityType)
	}
}

func TestHandleInboxLogsDecodeFailure(t *testing.T) {
	logger := logrus.StandardLogger()
	oldOutput := logger.Out
	oldFormatter := logger.Formatter
	oldLevel := logger.Level
	var output bytes.Buffer
	logger.SetOutput(&output)
	logger.SetFormatter(&logrus.TextFormatter{
		DisableColors:    true,
		DisableTimestamp: true,
	})
	logger.SetLevel(logrus.WarnLevel)
	t.Cleanup(func() {
		logger.SetOutput(oldOutput)
		logger.SetFormatter(oldFormatter)
		logger.SetLevel(oldLevel)
	})

	decoder := func(*http.Request) (*models.Activity, *models.Actor, []byte, error) {
		return nil, nil, nil, errors.New("category actor unavailable")
	}
	request := httptest.NewRequest(http.MethodPost, "/inbox", nil)
	request.RemoteAddr = "192.0.2.10:45678"
	request.Header.Set("User-Agent", "NodeBB/4.x")
	recorder := httptest.NewRecorder()

	handleInbox(recorder, request, decoder)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d; want %d", recorder.Code, http.StatusBadRequest)
	}
	logOutput := output.String()
	for _, expected := range []string{
		"Rejected inbox activity",
		"category actor unavailable",
		"192.0.2.10:45678",
		"NodeBB/4.x",
	} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("log output %q does not contain %q", logOutput, expected)
		}
	}
}
