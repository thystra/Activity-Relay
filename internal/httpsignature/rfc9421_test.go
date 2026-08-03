package httpsignature

import (
	"bytes"
	"context"
	"crypto/rsa"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/common-fate/httpsig/alg_rsa"
	"github.com/common-fate/httpsig/contentdigest"
	"github.com/common-fate/httpsig/sigbase"
	"github.com/common-fate/httpsig/sigset"
	legacyhttpsig "github.com/go-fed/httpsig"
)

func newSignedProfileTestRequest(
	t *testing.T,
	method string,
	body []byte,
) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	request, err := http.NewRequest(
		method,
		"https://remote.example:8443/inbox?view=full",
		reader,
	)
	if err != nil {
		t.Fatal(err)
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/activity+json")
	}
	return request
}

func parsedRFC9421Message(t *testing.T, request *http.Request) (
	[]string,
	string,
	string,
	error,
) {
	t.Helper()

	set, err := sigset.Unmarshal(request)
	if err != nil {
		return nil, "", "", err
	}
	message, err := set.Find(rfc9421SignatureTag)
	if err != nil {
		return nil, "", "", err
	}
	return message.Input.CoveredComponents,
		message.Input.KeyID,
		message.Input.Alg,
		nil
}

func verifyRFC9421Request(
	request *http.Request,
	publicKey *rsa.PublicKey,
) error {
	set, err := sigset.Unmarshal(request)
	if err != nil {
		return err
	}
	message, err := set.Find(rfc9421SignatureTag)
	if err != nil {
		return err
	}

	base, err := sigbase.Derive(
		message.Input,
		nil,
		request,
		contentdigest.SHA256,
	)
	if err != nil {
		return err
	}

	signingString, err := base.CanonicalString(message.Input)
	if err != nil {
		return err
	}

	return alg_rsa.NewRSAPKCS256Verifier(publicKey).Verify(
		context.Background(),
		signingString,
		message.Signature,
	)
}

func TestRFC9530ContentDigestSHA256(t *testing.T) {
	body := []byte(`{"type":"Follow"}`)

	got, err := RFC9530ContentDigestSHA256(body)
	if err != nil {
		t.Fatalf("generate Content-Digest: %v", err)
	}
	want := "sha-256=:GYwYnH3BiO6aICFt0ThC5bUIJ4byvqdpWtR8m5fNkww=:"
	if got != want {
		t.Fatalf("Content-Digest = %q; want %q", got, want)
	}

	if err := VerifyRFC9530ContentDigestSHA256([]string{got}, body); err != nil {
		t.Fatalf("verify Content-Digest: %v", err)
	}

	multiple := []string{
		"sha-512=:AA==:, " + got,
	}
	if err := VerifyRFC9530ContentDigestSHA256(multiple, body); err != nil {
		t.Fatalf("verify multi-algorithm Content-Digest: %v", err)
	}

	if err := VerifyRFC9530ContentDigestSHA256(
		[]string{got},
		[]byte(`{"type":"Create"}`),
	); err == nil {
		t.Fatal("tampered body unexpectedly passed Content-Digest verification")
	}
}

func TestSignGETWithRFC9421Profile(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	request := newSignedProfileTestRequest(t, http.MethodGet, nil)

	if err := signer.SignGETWithProfile(request, ProfileRFC9421); err != nil {
		t.Fatalf("sign RFC 9421 GET: %v", err)
	}

	if request.Host != "remote.example:8443" {
		t.Fatalf("wire authority = %q; want remote.example:8443", request.Host)
	}
	if request.Header.Get("Signature-Input") == "" {
		t.Fatal("RFC 9421 GET has no Signature-Input field")
	}
	if request.Header.Get("Signature") == "" {
		t.Fatal("RFC 9421 GET has no Signature field")
	}
	if request.Header.Get("Digest") != "" {
		t.Fatal("RFC 9421 GET unexpectedly retained a legacy Digest field")
	}
	if request.Header.Get("Content-Digest") != "" {
		t.Fatal("RFC 9421 GET unexpectedly has a Content-Digest field")
	}

	set, err := sigset.Unmarshal(request)
	if err != nil {
		t.Fatalf("parse RFC 9421 fields: %v", err)
	}
	message, err := set.Find(rfc9421SignatureTag)
	if err != nil {
		t.Fatalf("find ActivityPub signature: %v", err)
	}

	if message.Input.KeyID != "https://relay.example/actor#main-key" {
		t.Fatalf("key ID = %q", message.Input.KeyID)
	}
	if message.Input.Alg != alg_rsa.RSASSA_PKCS1_1_5_SHA256 {
		t.Fatalf("algorithm = %q", message.Input.Alg)
	}
	if message.Input.Tag != rfc9421SignatureTag {
		t.Fatalf("tag = %q", message.Input.Tag)
	}
	if message.Input.Nonce == "" {
		t.Fatal("RFC 9421 signature has no nonce")
	}
	if message.Input.Created.IsZero() {
		t.Fatal("RFC 9421 signature has no created parameter")
	}
	if !reflect.DeepEqual(
		message.Input.CoveredComponents,
		rfc9421GETComponents,
	) {
		t.Fatalf(
			"covered components = %#v; want %#v",
			message.Input.CoveredComponents,
			rfc9421GETComponents,
		)
	}

	if err := verifyRFC9421Request(request, publicKey); err != nil {
		t.Fatalf("verify RFC 9421 GET: %v", err)
	}
}

func TestSignGETWithRFC9421ProfileRemovesURLFragment(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	request, err := http.NewRequest(
		http.MethodGet,
		"https://remote.example/users/alan#main-key",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if request.URL.Fragment != "main-key" {
		t.Fatalf(
			"precondition fragment = %q; want main-key",
			request.URL.Fragment,
		)
	}

	if err := signer.SignGETWithProfile(
		request,
		ProfileRFC9421,
	); err != nil {
		t.Fatalf("sign fragment-bearing RFC 9421 GET: %v", err)
	}
	if request.URL.Fragment != "" || request.URL.RawFragment != "" {
		t.Fatalf(
			"signed URL retained fragment=%q raw_fragment=%q",
			request.URL.Fragment,
			request.URL.RawFragment,
		)
	}
	if got := request.URL.String(); got !=
		"https://remote.example/users/alan" {
		t.Fatalf("signed URL = %q", got)
	}
	if got := request.URL.RequestURI(); got != "/users/alan" {
		t.Fatalf("request URI = %q", got)
	}
	if err := verifyRFC9421Request(request, publicKey); err != nil {
		t.Fatalf("verify fragment-normalized RFC 9421 GET: %v", err)
	}
}

func TestSignPOSTWithRFC9421Profile(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	body := []byte(`{"type":"Follow"}`)
	request := newSignedProfileTestRequest(t, http.MethodPost, body)

	// Confirm the modern primitive removes stale legacy material.
	request.Header.Set("Digest", "SHA-256=stale")
	request.Header.Set("Signature", "stale-legacy-signature")

	if err := signer.SignPOSTWithProfile(
		request,
		body,
		ProfileRFC9421,
	); err != nil {
		t.Fatalf("sign RFC 9421 POST: %v", err)
	}

	if request.Header.Get("Digest") != "" {
		t.Fatal("RFC 9421 POST retained a legacy Digest field")
	}
	if request.Header.Get("Content-Digest") !=
		"sha-256=:GYwYnH3BiO6aICFt0ThC5bUIJ4byvqdpWtR8m5fNkww=:" {
		t.Fatalf(
			"Content-Digest = %q",
			request.Header.Get("Content-Digest"),
		)
	}
	if !strings.Contains(
		request.Header.Get("Signature-Input"),
		`alg="rsa-v1_5-sha256"`,
	) {
		t.Fatalf(
			"Signature-Input has wrong algorithm: %s",
			request.Header.Get("Signature-Input"),
		)
	}

	signedBody, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read body after signing: %v", err)
	}
	if !bytes.Equal(signedBody, body) {
		t.Fatalf("body changed during signing: %q", signedBody)
	}
	request.Body = io.NopCloser(bytes.NewReader(signedBody))

	if err := VerifyRFC9530ContentDigestSHA256(
		request.Header.Values("Content-Digest"),
		signedBody,
	); err != nil {
		t.Fatalf("verify RFC 9530 digest: %v", err)
	}

	covered, keyID, algorithm, err := parsedRFC9421Message(t, request)
	if err != nil {
		t.Fatalf("parse signed request: %v", err)
	}
	if !reflect.DeepEqual(covered, rfc9421POSTComponents) {
		t.Fatalf("covered components = %#v; want %#v", covered, rfc9421POSTComponents)
	}
	if keyID != "https://relay.example/actor#main-key" {
		t.Fatalf("key ID = %q", keyID)
	}
	if algorithm != alg_rsa.RSASSA_PKCS1_1_5_SHA256 {
		t.Fatalf("algorithm = %q", algorithm)
	}

	if err := verifyRFC9421Request(request, publicKey); err != nil {
		t.Fatalf("verify RFC 9421 POST: %v", err)
	}
}

func TestRFC9421SignatureRejectsTamperedBody(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	body := []byte(`{"type":"Follow"}`)
	request := newSignedProfileTestRequest(t, http.MethodPost, body)

	if err := signer.SignPOSTWithProfile(
		request,
		body,
		ProfileRFC9421,
	); err != nil {
		t.Fatalf("sign RFC 9421 POST: %v", err)
	}

	tampered := []byte(`{"type":"Create"}`)
	request.Body = io.NopCloser(bytes.NewReader(tampered))
	request.ContentLength = int64(len(tampered))

	if err := verifyRFC9421Request(request, publicKey); err == nil {
		t.Fatal("tampered body unexpectedly passed RFC 9421 verification")
	}

	if err := VerifyRFC9530ContentDigestSHA256(
		request.Header.Values("Content-Digest"),
		tampered,
	); err == nil {
		t.Fatal("tampered body unexpectedly passed RFC 9530 verification")
	}
}

func TestLegacyProfileRemainsEstablishedSigner(t *testing.T) {
	signer, publicKey := newTestSigner(t)
	body := []byte(`{"type":"Follow"}`)
	request := newSignedProfileTestRequest(t, http.MethodPost, body)

	if err := signer.SignPOSTWithProfile(
		request,
		body,
		ProfileLegacy,
	); err != nil {
		t.Fatalf("sign legacy POST: %v", err)
	}

	if request.Header.Get("Digest") == "" {
		t.Fatal("legacy profile has no Digest field")
	}
	if request.Header.Get("Content-Digest") != "" {
		t.Fatal("legacy profile unexpectedly has Content-Digest")
	}
	if request.Header.Get("Signature-Input") != "" {
		t.Fatal("legacy profile unexpectedly has Signature-Input")
	}
	if err := verifyRequestError(request, publicKey); err != nil {
		t.Fatalf("verify established legacy signature: %v", err)
	}
}

func verifyRequestError(
	request *http.Request,
	publicKey *rsa.PublicKey,
) error {
	verifier, err := legacyhttpsig.NewVerifier(request)
	if err != nil {
		return err
	}
	return verifier.Verify(publicKey, legacyhttpsig.RSA_SHA256)
}
