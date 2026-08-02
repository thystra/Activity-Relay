package deliver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/Songmu/go-httpdate"
	"github.com/common-fate/httpsig/alg_rsa"
	"github.com/common-fate/httpsig/contentdigest"
	"github.com/common-fate/httpsig/sigbase"
	"github.com/common-fate/httpsig/sigset"
	"github.com/go-fed/httpsig"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

func TestAppendSignature(t *testing.T) {
	file, _ := os.Open("../misc/test/create.json")
	body, _ := io.ReadAll(file)
	req, _ := http.NewRequest("POST", "https://localhost", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("Date", httpdate.Time2Str(time.Now()))
	appendSignature(req, &body, RelayActor.PublicKey.ID, GlobalConfig.ActorKey())
	if req.Header.Get("Signature-Input") != "" {
		t.Fatal("legacy delivery unexpectedly emitted Signature-Input")
	}

	// Activated compatibilityForHTTPSignature11
	sign := req.Header.Get("Signature")
	activated := regexp.MustCompile(`algorithm="` + string(httpsig.RSA_SHA256) + `"`).MatchString(sign)
	if !activated {
		t.Fatalf("Expected Signature header to contain algorithm=\"%s\", but got: %s", httpsig.RSA_SHA256, sign)
	}

	// Verify HTTPSignature
	verifier, err := httpsig.NewVerifier(req)
	if err != nil {
		t.Fatalf("Failed to create HTTPSignature verifier: %v", err)
	}
	err = verifier.Verify(GlobalConfig.ActorKey().Public(), httpsig.RSA_SHA256)
	if err != nil {
		t.Fatalf("HTTPSignature verification failed: %v", err)
	}

	// Verify Digest
	givenDigest := req.Header.Get("Digest")
	hash := sha256.New()
	hash.Write(body)
	b := hash.Sum(nil)
	calculatedDigest := "SHA-256=" + base64.StdEncoding.EncodeToString(b)

	if givenDigest != calculatedDigest {
		t.Fatalf("Expected Digest header to be '%s', but got '%s'", calculatedDigest, givenDigest)
	}
}

func verifyRFC9421Delivery(
	request *http.Request,
) error {
	set, err := sigset.Unmarshal(request)
	if err != nil {
		return err
	}
	message, err := set.Find("activitypub")
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
	return alg_rsa.NewRSAPKCS256Verifier(
		&GlobalConfig.ActorKey().PublicKey,
	).Verify(
		context.Background(),
		signingString,
		message.Signature,
	)
}

func TestAppendSignatureRFC9421(t *testing.T) {
	previous := OutboundRequestSigner
	t.Cleanup(func() {
		OutboundRequestSigner = previous
	})

	var err error
	OutboundRequestSigner, err = relayhttpsig.NewConfiguredSigner(
		RelayActor.PublicKey.ID,
		GlobalConfig.ActorKey(),
		relayhttpsig.ProfileRFC9421,
	)
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"type":"Announce"}`)
	request, err := http.NewRequest(
		http.MethodPost,
		"https://remote.example:8443/inbox",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/activity+json")
	request.Header.Set("Date", httpdate.Time2Str(time.Now()))

	if err := appendSignature(
		request,
		&body,
		RelayActor.PublicKey.ID,
		GlobalConfig.ActorKey(),
	); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Signature-Input") == "" {
		t.Fatal("RFC 9421 delivery has no Signature-Input")
	}
	if request.Header.Get("Content-Digest") == "" {
		t.Fatal("RFC 9421 delivery has no Content-Digest")
	}
	if request.Header.Get("Digest") != "" {
		t.Fatal("RFC 9421 delivery retained legacy Digest")
	}
	if err := verifyRFC9421Delivery(request); err != nil {
		t.Fatalf("verify RFC 9421 delivery: %v", err)
	}
}
