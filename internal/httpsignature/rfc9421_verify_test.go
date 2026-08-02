package httpsignature

import (
	"bytes"
	"context"
	"crypto/rsa"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type verificationTestResolver struct {
	key RFC9421ResolvedKey
	err error
}

func (resolver verificationTestResolver) ResolveRFC9421Key(
	context.Context,
	string,
) (RFC9421ResolvedKey, error) {
	return resolver.key, resolver.err
}

type verificationMemoryNonceStore struct {
	mu       sync.Mutex
	reserved map[string]struct{}
	calls    int
}

func newVerificationMemoryNonceStore() *verificationMemoryNonceStore {
	return &verificationMemoryNonceStore{
		reserved: make(map[string]struct{}),
	}
}

func (store *verificationMemoryNonceStore) ReserveRFC9421Nonce(
	_ context.Context,
	keyID string,
	nonce string,
	_ time.Duration,
) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	store.calls++
	key := keyID + "\x00" + nonce
	if _, exists := store.reserved[key]; exists {
		return false, nil
	}
	store.reserved[key] = struct{}{}
	return true, nil
}

func (store *verificationMemoryNonceStore) callCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.calls
}

func newInboundVerificationTestVerifier(
	t *testing.T,
	publicKey *rsa.PublicKey,
	store RFC9421NonceStore,
	now func() time.Time,
) *RFC9421Verifier {
	t.Helper()

	verifier, err := NewRFC9421Verifier(RFC9421VerifierOptions{
		Scheme:    "https",
		Authority: "remote.example:8443",
		KeyResolver: verificationTestResolver{
			key: RFC9421ResolvedKey{
				KeyID:     "https://relay.example/actor#main-key",
				Owner:     "https://relay.example/actor",
				ActorID:   "https://relay.example/actor",
				PublicKey: publicKey,
			},
		},
		NonceStore: store,
		Now:        now,
	})
	if err != nil {
		t.Fatalf("create RFC 9421 verifier: %v", err)
	}
	return verifier
}

func signedInboundVerificationRequest(
	t *testing.T,
	signer *Signer,
	body []byte,
) *http.Request {
	t.Helper()

	request := newSignedProfileTestRequest(t, http.MethodPost, body)
	if err := signer.SignPOSTWithProfile(
		request,
		body,
		ProfileRFC9421,
	); err != nil {
		t.Fatalf("sign RFC 9421 request: %v", err)
	}
	return request
}

func TestRFC9421VerifierAcceptsSignedPOSTAndBindsActor(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	store := newVerificationMemoryNonceStore()
	verifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		store,
		time.Now,
	)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := signedInboundVerificationRequest(t, signer, body)

	result, err := verifier.VerifyPOST(request, body)
	if err != nil {
		t.Fatalf("verify RFC 9421 POST: %v", err)
	}
	if result.KeyID != "https://relay.example/actor#main-key" {
		t.Fatalf("key ID = %q", result.KeyID)
	}
	if result.SignatureAlgorithm != "rsa-v1_5-sha256" {
		t.Fatalf("algorithm = %q", result.SignatureAlgorithm)
	}
	if err := result.BindActivityActor(
		"https://relay.example/actor",
	); err != nil {
		t.Fatalf("bind activity actor: %v", err)
	}
	if store.callCount() != 1 {
		t.Fatalf("nonce reservations = %d; want 1", store.callCount())
	}
}

func TestRFC9421VerifierRejectsReplay(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	store := newVerificationMemoryNonceStore()
	verifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		store,
		time.Now,
	)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := signedInboundVerificationRequest(t, signer, body)

	if _, err := verifier.VerifyPOST(request, body); err != nil {
		t.Fatalf("first verification: %v", err)
	}
	if _, err := verifier.VerifyPOST(request, body); !errors.Is(
		err,
		ErrRFC9421Replay,
	) {
		t.Fatalf("replay error = %v", err)
	}
}

func TestRFC9421VerifierDoesNotConsumeNonceForTamperedBody(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	store := newVerificationMemoryNonceStore()
	verifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		store,
		time.Now,
	)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := signedInboundVerificationRequest(t, signer, body)

	if _, err := verifier.VerifyPOST(
		request,
		[]byte(`{"actor":"https://relay.example/actor","type":"Create"}`),
	); err == nil {
		t.Fatal("tampered body unexpectedly verified")
	}
	if store.callCount() != 0 {
		t.Fatalf(
			"tampered body consumed %d nonce reservation(s)",
			store.callCount(),
		)
	}

	if _, err := verifier.VerifyPOST(request, body); err != nil {
		t.Fatalf("valid body after tamper attempt: %v", err)
	}
}

func TestRFC9421VerifierEnforcesCreatedWindow(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := signedInboundVerificationRequest(t, signer, body)

	staleVerifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		newVerificationMemoryNonceStore(),
		func() time.Time {
			return time.Now().Add(10 * time.Minute)
		},
	)
	if _, err := staleVerifier.VerifyPOST(request, body); err == nil ||
		!strings.Contains(err.Error(), "older than") {
		t.Fatalf("stale signature error = %v", err)
	}

	futureVerifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		newVerificationMemoryNonceStore(),
		func() time.Time {
			return time.Now().Add(-2 * time.Minute)
		},
	)
	if _, err := futureVerifier.VerifyPOST(request, body); err == nil ||
		!strings.Contains(err.Error(), "future") {
		t.Fatalf("future signature error = %v", err)
	}
}

func TestRFC9421VerifierRequiresActivityPubComponents(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	store := newVerificationMemoryNonceStore()
	verifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		store,
		time.Now,
	)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := newSignedProfileTestRequest(t, http.MethodPost, body)

	components := []string{
		"@method",
		"@authority",
		"@target-uri",
		"content-digest",
		"content-type",
	}
	if err := signer.signRFC9421(
		request,
		body,
		true,
		components,
	); err != nil {
		t.Fatalf("sign reduced-component request: %v", err)
	}

	if _, err := verifier.VerifyPOST(request, body); err == nil ||
		!strings.Contains(err.Error(), `"date"`) {
		t.Fatalf("missing component error = %v", err)
	}
	if store.callCount() != 0 {
		t.Fatalf("invalid component set consumed nonce: %d", store.callCount())
	}
}

func TestRFC9421VerifierRejectsWrongAuthority(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	verifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		newVerificationMemoryNonceStore(),
		time.Now,
	)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := signedInboundVerificationRequest(t, signer, body)
	request.Host = "other.example"

	if _, err := verifier.VerifyPOST(request, body); err == nil ||
		!strings.Contains(err.Error(), "authority") {
		t.Fatalf("authority error = %v", err)
	}
}

func TestRFC9421VerificationActorBinding(t *testing.T) {
	result := &RFC9421Verification{
		KeyOwner: "HTTPS://RELAY.EXAMPLE/actor",
		KeyActor: "https://relay.example/actor",
	}
	if err := result.BindActivityActor(
		"https://relay.example/actor",
	); err != nil {
		t.Fatalf("case-normalized identity binding: %v", err)
	}

	if err := result.BindActivityActor(
		"https://other.example/actor",
	); err == nil {
		t.Fatal("mismatched activity actor unexpectedly bound")
	}

	result.KeyActor = "https://relay.example/other"
	if err := result.BindActivityActor(
		"https://relay.example/actor",
	); err == nil {
		t.Fatal("mismatched resolved actor unexpectedly bound")
	}
}

func TestRFC9421VerifierRejectsInvalidSignatureBeforeNonce(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	store := newVerificationMemoryNonceStore()
	verifier := newInboundVerificationTestVerifier(
		t,
		publicKey,
		store,
		time.Now,
	)
	body := []byte(`{"actor":"https://relay.example/actor","type":"Follow"}`)
	request := signedInboundVerificationRequest(t, signer, body)

	signature := request.Header.Get("Signature")
	request.Header.Set(
		"Signature",
		strings.Replace(signature, ":",
			":AA", 1),
	)
	if _, err := verifier.VerifyPOST(request, body); err == nil {
		t.Fatal("invalid signature unexpectedly verified")
	}
	if store.callCount() != 0 {
		t.Fatalf("invalid signature consumed nonce: %d", store.callCount())
	}
}

func TestCloneRFC9421RequestPreservesBody(t *testing.T) {
	body := []byte("body")
	request := newSignedProfileTestRequest(t, http.MethodPost, body)
	cloned := cloneRFC9421Request(
		request,
		body,
		"https",
		"remote.example:8443",
	)
	got := new(bytes.Buffer)
	if _, err := got.ReadFrom(cloned.Body); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), body) {
		t.Fatalf("cloned body = %q; want %q", got.Bytes(), body)
	}
}
