package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/yukimochi/Activity-Relay/models"
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
	RelayState.SubscribersAndFollowers = []models.Subscriber{
		{Domain: "z.example"},
		{Domain: "a.example"},
		{Domain: "Z.EXAMPLE"},
		{Domain: ""},
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
	if got.ConnectedInstances.Count != 2 {
		t.Errorf("connected count = %d; want 2", got.ConnectedInstances.Count)
	}
	wantDomains := []string{"a.example", "z.example"}
	if !reflect.DeepEqual(got.ConnectedInstances.Domains, wantDomains) {
		t.Errorf("domains = %#v; want %#v", got.ConnectedInstances.Domains, wantDomains)
	}
	if got.Software.Version != "test-version" {
		t.Errorf("version = %q; want test-version", got.Software.Version)
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
