package models

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

type modelMemoryCapabilityStore struct {
	capability relayhttpsig.DestinationCapability
	found      bool
}

func (store *modelMemoryCapabilityStore) LoadDestinationCapability(
	_ context.Context,
	_ relayhttpsig.DestinationScope,
	_ string,
) (relayhttpsig.DestinationCapability, bool, error) {
	return store.capability, store.found, nil
}

func (store *modelMemoryCapabilityStore) SaveDestinationCapability(
	_ context.Context,
	capability relayhttpsig.DestinationCapability,
) (bool, error) {
	store.capability = capability
	store.found = true
	return true, nil
}

func TestFetchRemoteJSONUsesNegotiatingExecutor(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	store := &modelMemoryCapabilityStore{}
	negotiator, err := relayhttpsig.NewDestinationNegotiator(
		relayhttpsig.DestinationNegotiatorOptions{
			Store: store,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := relayhttpsig.NewNegotiatingSigner(
		"https://relay.example/actor#main-key",
		privateKey,
		relayhttpsig.ProfileDual,
		negotiator,
	)
	if err != nil {
		t.Fatal(err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			requests++
			if request.Header.Get("Signature-Input") != "" {
				writer.Header().Set(
					"WWW-Authenticate",
					`Signature realm="activitypub"`,
				)
				http.Error(
					writer,
					"legacy required",
					http.StatusUnauthorized,
				)
				return
			}
			writer.Header().Set(
				"Content-Type",
				"application/activity+json",
			)
			fmt.Fprint(
				writer,
				`{"id":"`+serverURL(request)+`/actor","type":"Service"}`,
			)
		},
	))
	defer server.Close()

	var actor Actor
	if _, err := fetchRemoteJSON(
		server.URL+"/actor",
		"test",
		&actor,
		signer,
	); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("negotiated fetch requests = %d; want 2", requests)
	}
}
