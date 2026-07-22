package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
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

type relayStatusResponse struct {
	SchemaVersion      int                  `json:"schema_version"`
	Status             string               `json:"status"`
	Name               string               `json:"name"`
	Domain             string               `json:"domain"`
	Registration       string               `json:"registration"`
	ManualApproval     bool                 `json:"manual_approval"`
	PersonOnly         bool                 `json:"person_only"`
	Endpoints          relayStatusEndpoints `json:"endpoints"`
	ConnectedInstances relayStatusInstances `json:"connected_instances"`
	Software           relayStatusSoftware  `json:"software"`
}

func buildRelayStatus() relayStatusResponse {
	baseURL := ""
	name := RelayActor.Name

	if GlobalConfig != nil {
		baseURL = strings.TrimRight(GlobalConfig.ServerHostname().String(), "/")
		if name == "" {
			name = GlobalConfig.ServerServiceName()
		}
	}

	registration := "open"
	if RelayState.RelayConfig.ManuallyAccept {
		registration = "approval_required"
	}

	seen := make(map[string]struct{}, len(RelayState.SubscribersAndFollowers))
	for _, instance := range RelayState.SubscribersAndFollowers {
		domain := strings.ToLower(strings.TrimSpace(instance.Domain))
		if domain == "" {
			continue
		}
		seen[domain] = struct{}{}
	}

	domains := make([]string, 0, len(seen))
	for domain := range seen {
		domains = append(domains, domain)
	}
	sort.Strings(domains)

	return relayStatusResponse{
		SchemaVersion:  1,
		Status:         "ok",
		Name:           name,
		Domain:         strings.TrimPrefix(baseURL, "https://"),
		Registration:   registration,
		ManualApproval: RelayState.RelayConfig.ManuallyAccept,
		PersonOnly:     RelayState.RelayConfig.PersonOnly,
		Endpoints: relayStatusEndpoints{
			Inbox: baseURL + "/inbox",
			Actor: baseURL + "/actor",
		},
		ConnectedInstances: relayStatusInstances{
			Count:   len(domains),
			Domains: domains,
		},
		Software: relayStatusSoftware{
			Name:       "activity-relay",
			Version:    version,
			Repository: "https://github.com/yukimochi/Activity-Relay",
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
