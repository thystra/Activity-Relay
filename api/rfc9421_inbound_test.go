package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/patrickmn/go-cache"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
	"github.com/thystra/Activity-Relay/models"
)

func runtimeTestPublicKeyPEM(
	t *testing.T,
	key *rsa.PublicKey,
) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: der,
	}))
}

func runtimeTestOrigin(
	t *testing.T,
	key *rsa.PrivateKey,
	mutate func(*models.Actor),
) (*httptest.Server, string) {
	t.Helper()

	var actor models.Actor
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/actor" {
				http.NotFound(writer, request)
				return
			}
			writer.Header().Set(
				"Content-Type",
				"application/activity+json",
			)
			if err := json.NewEncoder(writer).Encode(&actor); err != nil {
				t.Error(err)
			}
		},
	))
	t.Cleanup(server.Close)

	actorURL := server.URL + "/actor"
	actor = models.Actor{
		Context: []string{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		ID:        actorURL,
		Type:      "Application",
		Inbox:     server.URL + "/inbox",
		Endpoints: &models.Endpoints{SharedInbox: server.URL + "/inbox"},
		PublicKey: models.PublicKey{
			ID:           actorURL + "#main-key",
			Owner:        actorURL,
			PublicKeyPem: runtimeTestPublicKeyPEM(t, &key.PublicKey),
		},
	}
	if mutate != nil {
		mutate(&actor)
	}
	return server, actorURL
}

func preserveRFC9421RuntimeGlobals(t *testing.T) {
	t.Helper()

	oldActorCache := ActorCache
	oldVerifier := InboundRFC9421Verifier
	t.Cleanup(func() {
		ActorCache = oldActorCache
		InboundRFC9421Verifier = oldVerifier
	})
}

func runtimeTestVerifier(t *testing.T) {
	t.Helper()

	nonceStore, err := relayhttpsig.NewRedisRFC9421NonceStore(
		RelayState.RedisClient,
		"test:api:rfc9421:runtime:",
	)
	if err != nil {
		t.Fatal(err)
	}
	InboundRFC9421Verifier, err = relayhttpsig.NewRFC9421Verifier(
		relayhttpsig.RFC9421VerifierOptions{
			Scheme:      "https",
			Authority:   "relay.example",
			KeyResolver: activityPubRFC9421KeyResolver{},
			NonceStore:  nonceStore,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

func runtimeSignedRequest(
	t *testing.T,
	key *rsa.PrivateKey,
	actorURL string,
	body []byte,
) *http.Request {
	t.Helper()

	request, err := http.NewRequest(
		http.MethodPost,
		"https://relay.example/inbox",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "relay.example"
	request.Header.Set("Content-Type", "application/activity+json")

	signer, err := relayhttpsig.NewSigner(
		actorURL+"#main-key",
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.SignPOSTWithProfile(
		request,
		body,
		relayhttpsig.ProfileRFC9421,
	); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestDecodeActivityAcceptsRFC9421AndRejectsReplay(
	t *testing.T,
) {
	preserveRFC9421RuntimeGlobals(t)

	ctx := context.Background()
	if err := RelayState.RedisClient.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	ActorCache = cache.New(5*time.Minute, 10*time.Minute)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	_, actorURL := runtimeTestOrigin(t, key, nil)
	runtimeTestVerifier(t)

	body, err := json.Marshal(models.Activity{
		Context: "https://www.w3.org/ns/activitystreams",
		ID:      actorURL + "/activities/one",
		Actor:   actorURL,
		Type:    "Like",
		Object:  "https://example.invalid/object",
		To: []string{
			"https://www.w3.org/ns/activitystreams#Public",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := runtimeSignedRequest(t, key, actorURL, body)

	activity, actor, gotBody, err := decodeActivity(request)
	if err != nil {
		t.Fatalf("decode modern activity: %v", err)
	}
	if activity.Actor != actorURL || actor.ID != actorURL {
		t.Fatalf(
			"actor binding = activity %q actor %q",
			activity.Actor,
			actor.ID,
		)
	}
	if !bytes.Equal(gotBody, body) {
		t.Fatal("decoded body changed")
	}

	replay := request.Clone(context.Background())
	replay.Body = io.NopCloser(bytes.NewReader(body))
	replay.ContentLength = int64(len(body))
	if _, _, _, err := decodeActivity(replay); !errors.Is(
		err,
		relayhttpsig.ErrRFC9421Replay,
	) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestRFC9421KeyResolverRequiresExactPublicKeyID(t *testing.T) {
	preserveRFC9421RuntimeGlobals(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	_, actorURL := runtimeTestOrigin(
		t,
		key,
		func(actor *models.Actor) {
			actor.PublicKey.ID = actor.ID + "#other-key"
		},
	)

	ActorCache = cache.New(5*time.Minute, 10*time.Minute)
	_, err = (activityPubRFC9421KeyResolver{}).
		ResolveRFC9421Key(
			context.Background(),
			actorURL+"#main-key",
		)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("key mismatch error = %v", err)
	}
}

func TestModernProfileDoesNotDowngradeToLegacy(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"https://relay.example/inbox",
		strings.NewReader("{}"),
	)
	request.Host = "relay.example"
	request.Header["Signature-Input"] = []string{""}
	if selectInboundSignatureProfile(request) !=
		inboundSignatureProfileRFC9421 {
		t.Fatal("blank Signature-Input field did not select the modern profile")
	}
	request.Header.Set("Signature-Input", "not-a-structured-field")
	request.Header.Set(
		"Signature",
		`keyId="https://legacy.example/actor#main-key"`,
	)

	if selectInboundSignatureProfile(request) !=
		inboundSignatureProfileRFC9421 {
		t.Fatal("Signature-Input did not select the modern profile")
	}
	_, _, _, err := decodeActivity(request)
	if err == nil || strings.Contains(
		err.Error(),
		`neither "Signature" nor "Authorization"`,
	) {
		t.Fatalf("modern request fell back to legacy: %v", err)
	}
}
