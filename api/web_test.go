package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/thystra/Activity-Relay/models"
)

func TestHandleRelayStatus(t *testing.T) {
	originalState := RelayState
	originalActor := RelayActor
	originalVersion := version
	defer func() {
		RelayState = originalState
		RelayActor = originalActor
		version = originalVersion
	}()

	RelayActor.Name = "Test Relay"
	version = "test-version"
	RelayState.RelayConfig.ManuallyAccept = false
	RelayState.RelayConfig.PersonOnly = false
	RelayState.Subscribers = []models.Subscriber{{Domain: "a.example"}}
	RelayState.SubscribersAndFollowers = []models.Subscriber{
		{Domain: "z.example"},
		{Domain: "a.example"},
		{Domain: "Z.EXAMPLE"},
		{Domain: ""},
	}
	RelayState.Publishers = []models.Publisher{
		{
			Domain:           "publisher.example",
			FirstSeen:        "2026-07-26T17:30:13Z",
			LastSeen:         "2026-07-26T17:31:13Z",
			LastActivityType: "Update",
			ActivityCount:    2,
		},
		{Domain: "a.example", ActivityCount: 1},
	}

	req := httptest.NewRequest(http.MethodGet, "/status.json", nil)
	rec := httptest.NewRecorder()
	handleRelayStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d; want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	var got relayStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != "Test Relay" {
		t.Errorf("name = %q; want Test Relay", got.Name)
	}
	if got.Registration != "open" || got.ManualApproval {
		t.Errorf("registration = %q, manual_approval = %v", got.Registration, got.ManualApproval)
	}
	if got.SchemaVersion != 3 {
		t.Errorf("schema version = %d; want 3", got.SchemaVersion)
	}

	wantParticipating := []string{"a.example", "publisher.example", "z.example"}
	if got.ConnectedInstances.Count != len(wantParticipating) {
		t.Errorf("participating count = %d; want %d", got.ConnectedInstances.Count, len(wantParticipating))
	}
	if !reflect.DeepEqual(got.ConnectedInstances.Domains, wantParticipating) {
		t.Errorf("participating domains = %#v; want %#v", got.ConnectedInstances.Domains, wantParticipating)
	}

	wantReceiving := []string{"a.example", "z.example"}
	if got.ReceivingInstances.Count != len(wantReceiving) {
		t.Errorf("receiving count = %d; want %d", got.ReceivingInstances.Count, len(wantReceiving))
	}
	if !reflect.DeepEqual(got.ReceivingInstances.Domains, wantReceiving) {
		t.Errorf("receiving domains = %#v; want %#v", got.ReceivingInstances.Domains, wantReceiving)
	}

	if got.Publishers.Count != 2 {
		t.Fatalf("publisher count = %d; want 2", got.Publishers.Count)
	}
	if got.Publishers.Entries[0].Domain != "a.example" ||
		!got.Publishers.Entries[0].Subscribed ||
		!got.Publishers.Entries[0].ReceivesRelay {
		t.Errorf("unexpected receiving publisher entry: %+v", got.Publishers.Entries[0])
	}
	if got.Publishers.Entries[1].Domain != "publisher.example" ||
		got.Publishers.Entries[1].Subscribed ||
		got.Publishers.Entries[1].ReceivesRelay {
		t.Errorf("unexpected send-only publisher entry: %+v", got.Publishers.Entries[1])
	}
	if got.Software.Version != "test-version" {
		t.Errorf("version = %q; want test-version", got.Software.Version)
	}
	if got.Software.Repository != "https://github.com/thystra/Activity-Relay" {
		t.Errorf("repository = %q; want maintained fork URL", got.Software.Repository)
	}
}

func TestHandleRelayStatusMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/status.json", nil)
	rec := httptest.NewRecorder()
	handleRelayStatus(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d; want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q; want GET, HEAD", got)
	}
}

func TestHandleRelayStatusHead(t *testing.T) {
	req := httptest.NewRequest(http.MethodHead, "/status.json", nil)
	rec := httptest.NewRecorder()
	handleRelayStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d; want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD body length = %d; want 0", rec.Body.Len())
	}
}
