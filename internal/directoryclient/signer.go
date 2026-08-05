package directoryclient

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/common-fate/httpsig/alg_rsa"
	"github.com/common-fate/httpsig/sigbase"
	"github.com/common-fate/httpsig/signature"
	"github.com/common-fate/httpsig/sigparams"
	"github.com/common-fate/httpsig/sigset"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

const (
	SignatureLabel = "directory"
	SignatureTag   = "activity-relay-directory-v1"
	SignatureAlg   = alg_rsa.RSASSA_PKCS1_1_5_SHA256
	SignatureTTL   = 5 * time.Minute
)

var postComponents = []string{
	"@method",
	"@authority",
	"@target-uri",
	"content-digest",
	"content-type",
	"date",
}

type nonceSource func() (string, error)

type requestSigner struct {
	keyID      string
	privateKey *rsa.PrivateKey
	now        func() time.Time
	nonce      nonceSource
}

func newRequestSigner(
	keyID string,
	privateKey *rsa.PrivateKey,
	now func() time.Time,
	nonce nonceSource,
) (*requestSigner, error) {
	if keyID == "" || strings.TrimSpace(keyID) != keyID || privateKey == nil ||
		privateKey.N == nil || privateKey.N.BitLen() < 2048 {
		return nil, ErrDirectoryConfiguration
	}
	if now == nil {
		now = time.Now
	}
	if nonce == nil {
		nonce = randomNonce
	}
	return &requestSigner{keyID: keyID, privateKey: privateKey, now: now, nonce: nonce}, nil
}

func randomNonce() (string, error) {
	value := make([]byte, 24)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (signer *requestSigner) sign(request *http.Request, body []byte) error {
	if signer == nil || request == nil || request.URL == nil ||
		request.Method != http.MethodPost || request.URL.Scheme != "https" ||
		request.URL.Host == "" || request.URL.Fragment != "" {
		return ErrDirectoryConfiguration
	}
	created := time.Unix(signer.now().UTC().Unix(), 0).UTC()
	if created.Unix() < 0 {
		return ErrDirectoryConfiguration
	}
	nonce, err := signer.nonce()
	if err != nil || nonce == "" || len(nonce) > 256 || strings.ContainsAny(nonce, "\r\n") {
		return ErrDirectoryConfiguration
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Host = request.URL.Host
	request.Header.Set("Host", request.Host)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Date", created.Format(http.TimeFormat))
	digest, err := relayhttpsig.RFC9530ContentDigestSHA256(body)
	if err != nil {
		return fmt.Errorf("directory content digest: %w", err)
	}
	request.Header.Set("Content-Digest", digest)
	request.Header.Del("Digest")
	request.Header.Del("Signature")
	request.Header.Del("Signature-Input")
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	algorithm := alg_rsa.NewRSAPKCS256Signer(signer.privateKey)
	params := sigparams.Params{
		KeyID:             signer.keyID,
		Tag:               SignatureTag,
		Alg:               SignatureAlg,
		Created:           created,
		Expires:           created.Add(SignatureTTL),
		CoveredComponents: append([]string(nil), postComponents...),
		Nonce:             nonce,
	}
	base, err := sigbase.Derive(params, nil, request, algorithm.ContentDigest())
	if err != nil {
		return fmt.Errorf("derive directory signature: %w", err)
	}
	canonical, err := base.CanonicalString(params)
	if err != nil {
		return fmt.Errorf("canonicalize directory signature: %w", err)
	}
	signed, err := algorithm.Sign(request.Context(), canonical)
	if err != nil {
		return fmt.Errorf("sign directory request: %w", err)
	}
	set := &sigset.Set{Messages: map[string]*signature.Message{
		SignatureLabel: {Input: params, Signature: signed},
	}}
	if err := set.Include(request); err != nil {
		return fmt.Errorf("include directory signature: %w", err)
	}
	if request.Header.Get("Signature-Input") == "" || request.Header.Get("Signature") == "" {
		return errors.New("directory signature fields are missing")
	}
	return nil
}
