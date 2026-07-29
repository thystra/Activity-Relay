package models

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-fed/httpsig"
	"github.com/patrickmn/go-cache"
	"github.com/thystra/Activity-Relay/internal/httpsignature"
)

func newRemoteSignerForTest(t *testing.T) (*httpsignature.Signer, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	signer, err := httpsignature.NewSigner(
		"https://relay.example/actor#main-key",
		privateKey,
	)
	if err != nil {
		t.Fatalf("create test signer: %v", err)
	}
	return signer, &privateKey.PublicKey
}

func requireValidRemoteGET(request *http.Request, publicKey *rsa.PublicKey) error {
	if request.Method != http.MethodGet {
		return fmt.Errorf("method = %s; want GET", request.Method)
	}
	if request.Header.Get("Date") == "" {
		return fmt.Errorf("missing Date header")
	}
	if request.Header.Get("Signature") == "" {
		return fmt.Errorf("missing Signature header")
	}
	request.Header.Set("Host", request.Host)
	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		return fmt.Errorf("create verifier: %w", err)
	}
	if err := verifier.Verify(publicKey, httpsig.RSA_SHA256); err != nil {
		return fmt.Errorf("verify signature: %w", err)
	}
	return nil
}

func TestNewActivityPubActorFromRemoteActorSignsGET(t *testing.T) {
	signer, publicKey := newRemoteSignerForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := requireValidRemoteGET(request, publicKey); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		fmt.Fprint(w, `{"id":"`+serverURL(request)+`/actor","type":"Service"}`)
	}))
	defer server.Close()

	actor, err := NewActivityPubActorFromRemoteActor(
		server.URL+"/actor",
		"Activity-Relay test",
		cache.New(5*time.Minute, 10*time.Minute),
		signer,
	)
	if err != nil {
		t.Fatalf("fetch actor: %v", err)
	}
	if actor.ID != server.URL+"/actor" {
		t.Fatalf("actor ID = %q; want %q", actor.ID, server.URL+"/actor")
	}
}

func TestNewActivityPubActivityFromRemoteActivitySignsGET(t *testing.T) {
	signer, publicKey := newRemoteSignerForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := requireValidRemoteGET(request, publicKey); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/activity+json")
		fmt.Fprint(w, `{"id":"`+serverURL(request)+`/activities/1","actor":"https://origin.example/actor","type":"Create"}`)
	}))
	defer server.Close()

	activity, err := NewActivityPubActivityFromRemoteActivity(
		server.URL+"/activities/1",
		"Activity-Relay test",
		signer,
	)
	if err != nil {
		t.Fatalf("fetch activity: %v", err)
	}
	if activity.ID != server.URL+"/activities/1" {
		t.Fatalf("activity ID = %q; want %q", activity.ID, server.URL+"/activities/1")
	}
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}

func TestFetchRemoteJSONResignsRedirect(t *testing.T) {
	signer, publicKey := newRemoteSignerForTest(t)
	var verifiedRequests atomic.Int32

	finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := requireValidRemoteGET(request, publicKey); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		verifiedRequests.Add(1)
		w.Header().Set("Content-Type", "application/activity+json")
		fmt.Fprint(w, `{"id":"`+serverURL(request)+`/actor","type":"Service"}`)
	}))
	defer finalServer.Close()

	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if err := requireValidRemoteGET(request, publicKey); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		verifiedRequests.Add(1)
		http.Redirect(w, request, finalServer.URL+"/actor", http.StatusFound)
	}))
	defer redirectServer.Close()

	var actor Actor
	if _, err := fetchRemoteJSON(redirectServer.URL+"/actor", "test", &actor, signer); err != nil {
		t.Fatalf("fetch redirected actor: %v", err)
	}
	if actor.ID != finalServer.URL+"/actor" {
		t.Fatalf("actor ID = %q; want %q", actor.ID, finalServer.URL+"/actor")
	}
	if got := verifiedRequests.Load(); got != 2 {
		t.Fatalf("verified requests = %d; want 2", got)
	}
}

func TestFetchRemoteJSONRejectsOversizedResponse(t *testing.T) {
	signer, _ := newRemoteSignerForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/activity+json")
		fmt.Fprint(w, strings.Repeat("x", int(maxRemoteJSONBytes)+1))
	}))
	defer server.Close()
	var actor Actor
	_, err := fetchRemoteJSON(server.URL, "test", &actor, signer)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversized response error, got %v", err)
	}
}

func TestFetchRemoteJSONBoundsNonSuccessResponse(t *testing.T) {
	signer, _ := newRemoteSignerForTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, strings.Repeat("authorized fetch required ", 500))
	}))
	defer server.Close()
	var actor Actor
	_, err := fetchRemoteJSON(server.URL, "test", &actor, signer)
	if err == nil {
		t.Fatal("expected remote status error")
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "[truncated]") {
		t.Fatalf("expected bounded 401 body, got %v", err)
	}
	if len(err.Error()) > int(maxRemoteErrorBodyBytes)+100 {
		t.Fatalf("remote status error is not bounded: %d bytes", len(err.Error()))
	}
}

func TestFetchRemoteJSONRejectsUnsupportedScheme(t *testing.T) {
	signer, _ := newRemoteSignerForTest(t)
	var actor Actor
	_, err := fetchRemoteJSON("file:///etc/passwd", "test", &actor, signer)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported scheme error, got %v", err)
	}
}

func TestFetchRemoteJSONRequiresSigner(t *testing.T) {
	var actor Actor
	_, err := fetchRemoteJSON("https://remote.example/actor", "test", &actor, nil)
	if err == nil || !strings.Contains(err.Error(), "signer") {
		t.Fatalf("expected missing signer error, got %v", err)
	}
}
