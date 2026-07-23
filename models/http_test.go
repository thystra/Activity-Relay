package models

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchRemoteJSONRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/activity+json")
		fmt.Fprint(w, strings.Repeat("x", int(maxRemoteJSONBytes)+1))
	}))
	defer server.Close()

	var actor Actor
	_, err := fetchRemoteJSON(server.URL, "test", &actor)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestFetchRemoteJSONRejectsUnsupportedScheme(t *testing.T) {
	var actor Actor
	_, err := fetchRemoteJSON("file:///etc/passwd", "test", &actor)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported scheme error, got %v", err)
	}
}
