package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/patrickmn/go-cache"
)

const maxRemoteJSONBytes int64 = 2 * 1024 * 1024
const maxRemoteErrorBodyBytes int64 = 4096

// RemoteRequestSigner signs relay-authenticated remote GET requests.
type RemoteRequestSigner interface {
	SignGET(*http.Request) error
}

var remoteHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: func() http.RoundTripper {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.MaxIdleConns = 100
		transport.MaxIdleConnsPerHost = 8
		transport.IdleConnTimeout = 90 * time.Second
		transport.ResponseHeaderTimeout = 5 * time.Second
		return transport
	}(),
}

func fetchRemoteJSON(address string, uaString string, destination interface{}, signer RemoteRequestSigner) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("create remote request: %w", err)
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported remote URL scheme %q", req.URL.Scheme)
	}
	req.Header.Set("Accept", "application/activity+json")
	req.Header.Set("User-Agent", uaString)
	if signer == nil {
		return nil, errors.New("remote request signer is not configured")
	}
	if err := signer.SignGET(req); err != nil {
		return nil, fmt.Errorf("sign remote request: %w", err)
	}
	client := *remoteHTTPClient
	client.CheckRedirect = func(redirect *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		redirect.Header.Del("Date")
		redirect.Header.Del("Digest")
		redirect.Header.Del("Signature")
		if err := signer.SignGET(redirect); err != nil {
			return fmt.Errorf("sign redirected remote request: %w", err)
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, readErr := io.ReadAll(
			io.LimitReader(resp.Body, maxRemoteErrorBodyBytes+1),
		)
		if readErr != nil {
			return nil, fmt.Errorf("%s: unable to read response body: %w", resp.Status, readErr)
		}
		responseTruncated := int64(len(responseBody)) > maxRemoteErrorBodyBytes
		if responseTruncated {
			responseBody = responseBody[:maxRemoteErrorBodyBytes]
		}
		responseText := strings.Join(strings.Fields(string(responseBody)), " ")
		if responseTruncated && responseText != "" {
			responseText += " [truncated]"
		}
		if responseText != "" {
			return nil, fmt.Errorf("%s: %s", resp.Status, responseText)
		}
		return nil, errors.New(resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteJSONBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read remote response: %w", err)
	}
	if int64(len(data)) > maxRemoteJSONBytes {
		return nil, errors.New("remote JSON response exceeds 2 MiB limit")
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return nil, err
	}
	return data, nil
}

// PublicKey : Activity Certificate.
type PublicKey struct {
	ID           string `json:"id,omitempty"`
	Owner        string `json:"owner,omitempty"`
	PublicKeyPem string `json:"publicKeyPem,omitempty"`
}

// Endpoints : Contains SharedInbox address.
type Endpoints struct {
	SharedInbox string `json:"sharedInbox,omitempty"`
}

// Image : Image Object.
type Image struct {
	URL string `json:"url,omitempty"`
}

// Actor : ActivityPub Actor.
type Actor struct {
	Context           interface{} `json:"@context,omitempty"`
	ID                string      `json:"id,omitempty"`
	Type              string      `json:"type,omitempty"`
	Name              string      `json:"name,omitempty"`
	PreferredUsername string      `json:"preferredUsername,omitempty"`
	Summary           string      `json:"summary,omitempty"`
	Inbox             string      `json:"inbox,omitempty"`
	Outbox            string      `json:"outbox,omitempty"`
	FollowingURL      string      `json:"following,omitempty"`
	FollowersURL      string      `json:"followers,omitempty"`
	Endpoints         *Endpoints  `json:"endpoints,omitempty"`
	PublicKey         PublicKey   `json:"publicKey,omitempty"`
	Icon              *Image      `json:"icon,omitempty"`
	Image             *Image      `json:"image,omitempty"`
}

// Followers : ActivityPub Terms for Actor's Followers.
func (actor *Actor) Followers() string {
	if actor.FollowersURL != "" {
		return actor.FollowersURL
	}
	return actor.ID + "/followers"
}

// NewActivityPubActorFromRelayConfig : Create Actor from relay config.
func NewActivityPubActorFromRelayConfig(globalConfig *RelayConfig) Actor {
	hostname := globalConfig.domain.String()
	publicKey := &globalConfig.actorKey.PublicKey
	publicKeyPemString := generatePublicKeyPEMString(publicKey)

	newActor := Actor{
		Context:           []string{"https://www.w3.org/ns/activitystreams", "https://w3id.org/security/v1"},
		ID:                hostname + "/actor",
		Type:              "Application",
		Name:              globalConfig.serviceName,
		PreferredUsername: "relay",
		Summary:           globalConfig.serviceSummary,
		Inbox:             hostname + "/inbox",
		Outbox:            hostname + "/actor/outbox",
		FollowingURL:      hostname + "/actor/following",
		FollowersURL:      hostname + "/actor/followers",
		Endpoints: &Endpoints{
			SharedInbox: hostname + "/inbox",
		},
		PublicKey: PublicKey{
			ID:           hostname + "/actor#main-key",
			Owner:        hostname + "/actor",
			PublicKeyPem: publicKeyPemString,
		},
	}

	if globalConfig.serviceIconURL != nil {
		newActor.Icon = &Image{
			URL: globalConfig.serviceIconURL.String(),
		}
	}
	if globalConfig.serviceImageURL != nil {
		newActor.Image = &Image{
			URL: globalConfig.serviceImageURL.String(),
		}
	}

	return newActor
}

// NewActivityPubActorFromRemoteActor : Retrieve Actor from remote instance.
func NewActivityPubActorFromRemoteActor(url string, uaString string, cache *cache.Cache, signer RemoteRequestSigner) (Actor, error) {
	var actor = new(Actor)
	var err error
	cacheData, found := cache.Get(url)
	if found {
		data, ok := cacheData.([]byte)
		if !ok {
			cache.Delete(url)
		} else {
			err = json.Unmarshal(data, actor)
		}
		if err != nil {
			cache.Delete(url)
		} else if ok {
			return *actor, nil
		}
	}
	data, err := fetchRemoteJSON(url, uaString, actor, signer)
	if err != nil {
		return *actor, err
	}
	cache.Set(url, data, 5*time.Minute)
	return *actor, nil
}

// Activity : ActivityPub Activity.
type Activity struct {
	Context interface{} `json:"@context,omitempty"`
	ID      string      `json:"id,omitempty"`
	Actor   string      `json:"actor,omitempty"`
	Type    string      `json:"type,omitempty"`
	Object  interface{} `json:"object,omitempty"`
	To      []string    `json:"to,omitempty"`
	Cc      []string    `json:"cc,omitempty"`
}

// GenerateReply : Generate activity to activity's actor.
func (activity *Activity) GenerateReply(actor Actor, object interface{}, activityType string) Activity {
	return Activity{
		[]string{"https://www.w3.org/ns/activitystreams"},
		actor.ID + "/activities/" + uuid.New().String(),
		actor.ID,
		activityType,
		object,
		[]string{activity.Actor},
		nil,
	}
}

// UnwrapInnerActivity : Unwrap inner activity.
func (activity *Activity) UnwrapInnerActivity() (*Activity, error) {
	switch innerActivity := activity.Object.(type) {
	case map[string]interface{}:
		innerId, IdOk := innerActivity["id"].(string)
		innerType, TypeOk := innerActivity["type"].(string)
		innerActor, ActorOk := innerActivity["actor"].(string)
		innerObject, ActivityOk := innerActivity["object"]

		if IdOk && TypeOk && ActorOk && ActivityOk {
			switch object := innerActivity["object"].(type) {
			case string:
				return &Activity{
					ID:     innerId,
					Type:   innerType,
					Actor:  innerActor,
					Object: object,
				}, nil
			default:
				return &Activity{
					ID:     innerId,
					Type:   innerType,
					Actor:  innerActor,
					Object: innerObject,
				}, nil
			}
		}
	}
	return nil, errors.New("object is not Activity")
}

// UnwrapInnerObjectId : Unwrap inner object id.
func (activity *Activity) UnwrapInnerObjectId() (string, error) {
	switch innerObject := activity.Object.(type) {
	case string:
		return innerObject, nil
	case map[string]interface{}:
		innerId, IdOk := innerObject["id"].(string)
		if IdOk {
			return innerId, nil
		}
	}
	return "", errors.New("object not has id")
}

// NewActivityPubActivity : Generate activity.
func NewActivityPubActivity(actor Actor, to []string, object interface{}, activityType string) Activity {
	return Activity{
		[]string{"https://www.w3.org/ns/activitystreams"},
		actor.ID + "/activities/" + uuid.New().String(),
		actor.ID,
		activityType,
		object,
		to,
		nil,
	}
}

// NewActivityPubActivityFromRemoteActivity : Retrieve Activity from remote instance.
func NewActivityPubActivityFromRemoteActivity(url string, uaString string, signer RemoteRequestSigner) (Activity, error) {
	var activity = new(Activity)
	var err error
	if _, err = fetchRemoteJSON(url, uaString, activity, signer); err != nil {
		return *activity, err
	}
	return *activity, nil
}

// Signature : ActivityPub Header Signature.
type Signature struct {
	Type           string `json:"type,omitempty"`
	Creator        string `json:"creator,omitempty"`
	Created        string `json:"created,omitempty"`
	SignatureValue string `json:"signatureValue,omitempty"`
}

// WebfingerLink : Webfinger Link Resource.
type WebfingerLink struct {
	Rel  string `json:"rel,omitempty"`
	Type string `json:"type,omitempty"`
	Href string `json:"href,omitempty"`
}

// WebfingerResource : Webfinger Resource.
type WebfingerResource struct {
	Subject string          `json:"subject,omitempty"`
	Links   []WebfingerLink `json:"links,omitempty"`
}

// GenerateWebfingerResource : Generate Webfinger resource.
func (actor *Actor) GenerateWebfingerResource(hostname *url.URL) WebfingerResource {
	resource := new(WebfingerResource)

	resource.Subject = "acct:" + actor.PreferredUsername + "@" + hostname.Host
	resource.Links = []WebfingerLink{
		{
			"self",
			"application/activity+json",
			actor.ID,
		},
	}
	return *resource
}

// NodeinfoResources : Nodeinfo Resources.
type NodeinfoResources struct {
	NodeinfoLinks NodeinfoLinks
	Nodeinfo      Nodeinfo
}

// NodeinfoLinks : Nodeinfo Link Resource.
type NodeinfoLinks struct {
	Links []NodeinfoLink `json:"links"`
}

// NodeinfoLink : Nodeinfo Link Resource.
type NodeinfoLink struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
}

// Nodeinfo : Nodeinfo Resource.
type Nodeinfo struct {
	Version           string           `json:"version"`
	Software          NodeinfoSoftware `json:"software"`
	Protocols         []string         `json:"protocols"`
	Services          NodeinfoServices `json:"services"`
	OpenRegistrations bool             `json:"openRegistrations"`
	Usage             NodeinfoUsage    `json:"usage"`
	Metadata          NodeinfoMetadata `json:"metadata"`
}

// NodeinfoSoftware : NodeinfoSoftware Resource.
type NodeinfoSoftware struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Repository string `json:"repository,omitempty"`
}

// NodeinfoServices : NodeinfoSoftware Resource.
type NodeinfoServices struct {
	Inbound  []string `json:"inbound"`
	Outbound []string `json:"outbound"`
}

// NodeinfoUsage : NodeinfoUsage Resource.
type NodeinfoUsage struct {
	Users NodeinfoUsageUsers `json:"users"`
}

// NodeinfoUsageUsers : NodeinfoUsageUsers Resource.
type NodeinfoUsageUsers struct {
	Total          int `json:"total"`
	ActiveMonth    int `json:"activeMonth"`
	ActiveHalfyear int `json:"activeHalfyear"`
}

// NodeinfoMetadata : NodeinfoMetadata Resource.
type NodeinfoMetadata struct {
}

// GenerateNodeinfoResources : Generate Nodeinfo resources.
func GenerateNodeinfoResources(hostname *url.URL, serverVersion string) NodeinfoResources {
	resources := new(NodeinfoResources)

	resources.NodeinfoLinks.Links = []NodeinfoLink{
		{
			"http://nodeinfo.diaspora.software/ns/schema/2.1",
			"https://" + hostname.Host + "/nodeinfo/2.1",
		},
	}
	resources.Nodeinfo = Nodeinfo{
		"2.1",
		NodeinfoSoftware{"activity-relay", serverVersion, "https://github.com/thystra/Activity-Relay"},
		[]string{"activitypub"},
		NodeinfoServices{[]string{}, []string{}},
		true,
		NodeinfoUsage{NodeinfoUsageUsers{0, 0, 0}},
		NodeinfoMetadata{},
	}

	return *resources
}
