package httpsignature

import (
	"crypto/rsa"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Songmu/go-httpdate"
	"github.com/go-fed/httpsig"
)

var hs2019Pattern = regexp.MustCompile(`algorithm="hs2019"`)

// Signer signs ActivityPub HTTP requests with the relay actor identity.
type Signer struct {
	keyID      string
	privateKey *rsa.PrivateKey
}

// NewSigner validates and returns an ActivityPub HTTP request signer.
func NewSigner(keyID string, privateKey *rsa.PrivateKey) (*Signer, error) {
	if strings.TrimSpace(keyID) == "" {
		return nil, errors.New("HTTP signature signer has no key ID")
	}
	if privateKey == nil {
		return nil, errors.New("HTTP signature signer has no private key")
	}
	return &Signer{keyID: keyID, privateKey: privateKey}, nil
}

// SignGET signs an authorized-fetch GET request.
func (signer *Signer) SignGET(request *http.Request) error {
	return signer.sign(
		request,
		nil,
		[]string{httpsig.RequestTarget, "Host", "Date"},
	)
}

// SignPOST signs an ActivityPub delivery request and its body digest.
func (signer *Signer) SignPOST(request *http.Request, body []byte) error {
	return signer.sign(
		request,
		body,
		[]string{httpsig.RequestTarget, "Host", "Date", "Digest", "Content-Type"},
	)
}

func (signer *Signer) sign(request *http.Request, body []byte, headers []string) error {
	if signer == nil || signer.privateKey == nil || strings.TrimSpace(signer.keyID) == "" {
		return errors.New("HTTP signature signer is not initialized")
	}
	if request == nil || request.URL == nil || request.URL.Host == "" {
		return errors.New("signed request has no URL host")
	}
	if request.Header == nil {
		request.Header = make(http.Header)
	}

	// net/http treats Host specially. Sign the exact authority placed on the
	// wire, including a non-default port, and expose it to go-fed/httpsig.
	request.Host = request.URL.Host
	request.Header.Set("Host", request.Host)
	if request.Header.Get("Date") == "" {
		request.Header.Set("Date", httpdate.Time2Str(time.Now()))
	}

	httpSigner, _, err := httpsig.NewSigner(
		[]httpsig.Algorithm{httpsig.RSA_SHA256},
		httpsig.DigestSha256,
		headers,
		httpsig.Signature,
		60*60,
	)
	if err != nil {
		return err
	}
	if err := httpSigner.SignRequest(signer.privateKey, signer.keyID, request, body); err != nil {
		return err
	}

	// Compatibility for implementations that still reject the hs2019 token.
	signature := request.Header.Get("Signature")
	signature = hs2019Pattern.ReplaceAllString(
		signature,
		`algorithm="`+string(httpsig.RSA_SHA256)+`"`,
	)
	request.Header.Set("Signature", signature)
	return nil
}
