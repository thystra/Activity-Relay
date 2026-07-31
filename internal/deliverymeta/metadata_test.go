// File: internal/deliverymeta/metadata_test.go
package deliverymeta

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestFromBodyExtractsPublicCorrelationMetadata(t *testing.T) {
	body := []byte(`{
		"id":"https://origin.example/activities/1",
		"type":"Announce",
		"actor":"https://origin.example/users/alice",
		"object":{"id":"https://origin.example/objects/2"}
	}`)

	got := FromBody(body)
	if got.ActivityID != "https://origin.example/activities/1" {
		t.Fatalf("activity ID = %q", got.ActivityID)
	}
	if got.ActivityType != "Announce" {
		t.Fatalf("activity type = %q", got.ActivityType)
	}
	if got.ActorID != "https://origin.example/users/alice" {
		t.Fatalf("actor ID = %q", got.ActorID)
	}
	if got.OriginDomain != "origin.example" {
		t.Fatalf("origin domain = %q", got.OriginDomain)
	}
	if got.ObjectID != "https://origin.example/objects/2" {
		t.Fatalf("object ID = %q", got.ObjectID)
	}
	if got.BodySHA256 == "" {
		t.Fatal("body SHA-256 is empty")
	}
}

func TestFromBodyKeepsDigestForMalformedJSON(t *testing.T) {
	body := []byte(`not-json`)
	sum := sha256.Sum256(body)

	got := FromBody(body)
	if got.BodySHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("body SHA-256 = %q", got.BodySHA256)
	}
	if got.ActivityID != "" || got.ActorID != "" || got.ObjectID != "" {
		t.Fatalf("unexpected metadata for malformed JSON: %+v", got)
	}
}

func TestFromBodyBoundsIdentifiers(t *testing.T) {
	longID := "https://origin.example/" + strings.Repeat("x", maxIdentifierBytes)
	body := []byte(`{"id":"` + longID + `"}`)

	got := FromBody(body)
	if len(got.ActivityID) != maxIdentifierBytes {
		t.Fatalf("bounded activity ID length = %d; want %d", len(got.ActivityID), maxIdentifierBytes)
	}
}

// EOF: internal/deliverymeta/metadata_test.go
