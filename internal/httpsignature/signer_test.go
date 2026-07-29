package httpsignature

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"strings"
	"testing"

	"github.com/go-fed/httpsig"
)

func newTestSigner(t *testing.T) (*Signer, *rsa.PublicKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	signer, err := NewSigner("https://relay.example/actor#main-key", privateKey)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}
	return signer, &privateKey.PublicKey
}

func verifyRequest(t *testing.T, request *http.Request, publicKey *rsa.PublicKey) {
	t.Helper()
	verifier, err := httpsig.NewVerifier(request)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if err := verifier.Verify(publicKey, httpsig.RSA_SHA256); err != nil {
		t.Fatalf("verify signature: %v", err)
	}
}

func TestSignGETUsesExactWireAuthority(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	request, err := http.NewRequest(
		http.MethodGet,
		"https://remote.example:8443/actor?view=full",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := signer.SignGET(request); err != nil {
		t.Fatalf("sign GET: %v", err)
	}
	if request.Host != "remote.example:8443" {
		t.Fatalf("request host = %q; want remote.example:8443", request.Host)
	}
	if request.Header.Get("Host") != request.Host {
		t.Fatalf("signed Host = %q; wire Host = %q", request.Header.Get("Host"), request.Host)
	}
	if request.Header.Get("Date") == "" {
		t.Fatal("signed GET has no Date header")
	}
	if request.Header.Get("Digest") != "" {
		t.Fatal("signed GET unexpectedly has a Digest header")
	}
	signature := request.Header.Get("Signature")
	if !strings.Contains(signature, `algorithm="rsa-sha256"`) {
		t.Fatalf("signature does not advertise rsa-sha256: %s", signature)
	}
	if !strings.Contains(strings.ToLower(signature), `headers="(request-target) host date"`) {
		t.Fatalf("GET signed-header set is incorrect: %s", signature)
	}
	verifyRequest(t, request, publicKey)
}

func TestSignPOSTIncludesDigestAndContentType(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	body := []byte(`{"type":"Follow"}`)
	request, err := http.NewRequest(
		http.MethodPost,
		"https://remote.example/inbox",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/activity+json")
	if err := signer.SignPOST(request, body); err != nil {
		t.Fatalf("sign POST: %v", err)
	}
	if request.Header.Get("Digest") == "" {
		t.Fatal("signed POST has no Digest header")
	}
	verifyRequest(t, request, publicKey)
}

func TestNewSignerRejectsMissingIdentity(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSigner("", privateKey); err == nil {
		t.Fatal("expected missing key ID error")
	}
	if _, err := NewSigner("https://relay.example/actor#main-key", nil); err == nil {
		t.Fatal("expected missing private key error")
	}
}
