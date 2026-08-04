package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/thystra/Activity-Relay/models"
)

type relayStatusEndpoints struct {
	Inbox string `json:"inbox"`
	Actor string `json:"actor"`
}

type relayStatusSoftware struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Repository string `json:"repository"`
}

type relayStatusInstances struct {
	Count   int      `json:"count"`
	Domains []string `json:"domains"`
}
type relayStatusReceiver struct {
	Domain              string `json:"domain"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	LastFailureAt       string `json:"last_failure_at,omitempty"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
	TotalSuccesses      int64  `json:"total_successes"`
	TotalFailures       int64  `json:"total_failures"`
}
type relayStatusReceivingInstances struct {
	Count   int                   `json:"count"`
	Domains []string              `json:"domains"`
	Entries []relayStatusReceiver `json:"entries"`
}

type relayStatusPublisher struct {
	Domain           string `json:"domain"`
	FirstSeen        string `json:"first_seen,omitempty"`
	LastSeen         string `json:"last_seen,omitempty"`
	LastActivityType string `json:"last_activity_type,omitempty"`
	ActivityCount    int64  `json:"activity_count"`
	Subscribed       bool   `json:"subscribed"`
	ReceivesRelay    bool   `json:"receives_relay"`
}

type relayStatusPublishers struct {
	Count   int                    `json:"count"`
	Entries []relayStatusPublisher `json:"entries"`
}

type relayStatusResponse struct {
	SchemaVersion                   int                                    `json:"schema_version"`
	Status                          string                                 `json:"status"`
	Name                            string                                 `json:"name"`
	Domain                          string                                 `json:"domain"`
	Registration                    string                                 `json:"registration"`
	ManualApproval                  bool                                   `json:"manual_approval"`
	PersonOnly                      bool                                   `json:"person_only"`
	PublicAddressDistributionPolicy models.PublicAddressDistributionPolicy `json:"public_address_distribution_policy"`
	PublicAddressDistributionLabel  string                                 `json:"public_address_distribution_label"`
	Endpoints                       relayStatusEndpoints                   `json:"endpoints"`
	ConnectedInstances              relayStatusInstances                   `json:"connected_instances"`
	ReceivingInstances              relayStatusReceivingInstances          `json:"receiving_instances"`
	Publishers                      relayStatusPublishers                  `json:"publishers"`
	Software                        relayStatusSoftware                    `json:"software"`
}

func normalizedStatusDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

func sortedStatusDomains(seen map[string]struct{}) []string {
	domains := make([]string, 0, len(seen))
	for domain := range seen {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func buildRelayStatus() relayStatusResponse {
	snapshot := RelayState.Snapshot()
	baseURL := ""
	name := RelayActor.Name
	publicAddressPolicy := models.PublicAddressPublicAndUnlisted

	if GlobalConfig != nil {
		baseURL = strings.TrimRight(GlobalConfig.ServerHostname().String(), "/")
		publicAddressPolicy = GlobalConfig.PublicAddressDistributionPolicy()
		if name == "" {
			name = GlobalConfig.ServerServiceName()
		}
	}

	registration := "open"
	if snapshot.RelayConfig.ManuallyAccept {
		registration = "approval_required"
	}

	receivingSeen := make(map[string]struct{}, len(snapshot.SubscribersAndFollowers))
	for _, instance := range snapshot.SubscribersAndFollowers {
		domain := normalizedStatusDomain(instance.Domain)
		if domain != "" {
			receivingSeen[domain] = struct{}{}
		}
	}

	subscribed := make(map[string]struct{}, len(snapshot.Subscribers))
	for _, subscriber := range snapshot.Subscribers {
		domain := normalizedStatusDomain(subscriber.Domain)
		if domain != "" {
			subscribed[domain] = struct{}{}
		}
	}

	participatingSeen := make(map[string]struct{}, len(receivingSeen)+len(snapshot.Publishers))
	for domain := range receivingSeen {
		participatingSeen[domain] = struct{}{}
	}

	publishers := make([]relayStatusPublisher, 0, len(snapshot.Publishers))
	for _, publisher := range snapshot.Publishers {
		domain := normalizedStatusDomain(publisher.Domain)
		if domain == "" {
			continue
		}
		participatingSeen[domain] = struct{}{}
		_, isSubscribed := subscribed[domain]
		_, receivesRelay := receivingSeen[domain]
		publishers = append(publishers, relayStatusPublisher{
			Domain:           domain,
			FirstSeen:        publisher.FirstSeen,
			LastSeen:         publisher.LastSeen,
			LastActivityType: publisher.LastActivityType,
			ActivityCount:    publisher.ActivityCount,
			Subscribed:       isSubscribed,
			ReceivesRelay:    receivesRelay,
		})
	}
	sort.Slice(publishers, func(i, j int) bool {
		return publishers[i].Domain < publishers[j].Domain
	})

	participatingDomains := sortedStatusDomains(participatingSeen)
	receivingDomains := sortedStatusDomains(receivingSeen)
	healthByDomain := make(map[string]models.ReceiverDeliveryHealth)
	if RelayState.RedisClient != nil {
		loadedHealth, err := models.LoadReceiverDeliveryHealth(
			context.Background(),
			RelayState.RedisClient,
			receivingDomains,
		)
		if err != nil {
			logrus.WithError(err).Warn("Unable to load receiver delivery health for public status")
		} else {
			healthByDomain = loadedHealth
		}
	}
	receiverEntries := make([]relayStatusReceiver, 0, len(receivingDomains))
	for _, domain := range receivingDomains {
		health := healthByDomain[domain]
		receiverEntries = append(receiverEntries, relayStatusReceiver{
			Domain:              domain,
			LastSuccessAt:       health.LastSuccessAt,
			LastFailureAt:       health.LastFailureAt,
			ConsecutiveFailures: health.ConsecutiveFailures,
			TotalSuccesses:      health.TotalSuccesses,
			TotalFailures:       health.TotalFailures,
		})
	}

	return relayStatusResponse{
		SchemaVersion:                   5,
		Status:                          "ok",
		Name:                            name,
		Domain:                          strings.TrimPrefix(baseURL, "https://"),
		Registration:                    registration,
		ManualApproval:                  snapshot.RelayConfig.ManuallyAccept,
		PersonOnly:                      snapshot.RelayConfig.PersonOnly,
		PublicAddressDistributionPolicy: publicAddressPolicy,
		PublicAddressDistributionLabel:  publicAddressPolicy.DisplayName(),
		Endpoints: relayStatusEndpoints{
			Inbox: baseURL + "/inbox",
			Actor: baseURL + "/actor",
		},
		ConnectedInstances: relayStatusInstances{
			Count:   len(participatingDomains),
			Domains: participatingDomains,
		},
		ReceivingInstances: relayStatusReceivingInstances{
			Count:   len(receivingDomains),
			Domains: receivingDomains,
			Entries: receiverEntries,
		},
		Publishers: relayStatusPublishers{
			Count:   len(publishers),
			Entries: publishers,
		},
		Software: relayStatusSoftware{
			Name:       "activity-relay",
			Version:    version,
			Repository: "https://github.com/thystra/Activity-Relay",
		},
	}
}

func handleRelayStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=30")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := json.NewEncoder(w).Encode(buildRelayStatus()); err != nil {
		http.Error(w, "failed to encode relay status", http.StatusInternalServerError)
	}
}
