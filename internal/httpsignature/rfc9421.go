package httpsignature

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Songmu/go-httpdate"
	"github.com/common-fate/httpsig/alg_rsa"
	"github.com/common-fate/httpsig/signature"
	rfc9421signer "github.com/common-fate/httpsig/signer"
	"github.com/common-fate/httpsig/sigset"
	"github.com/dunglas/httpsfv"
)

const (
	rfc9421SignatureLabel = "activitypub"
	rfc9421SignatureTag   = "activitypub"
)

var (
	rfc9421GETComponents = []string{
		"@method",
		"@authority",
		"@target-uri",
		"date",
	}
	rfc9421POSTComponents = []string{
		"@method",
		"@authority",
		"@target-uri",
		"content-digest",
		"content-type",
		"date",
	}
)

// RFC9530ContentDigestSHA256 returns a structured Content-Digest field value
// over the exact message content bytes.
func RFC9530ContentDigestSHA256(body []byte) (string, error) {
	sum := sha256.Sum256(body)
	dictionary := httpsfv.NewDictionary()
	dictionary.Add("sha-256", httpsfv.NewItem(sum[:]))

	value, err := httpsfv.Marshal(dictionary)
	if err != nil {
		return "", fmt.Errorf("marshal RFC 9530 Content-Digest: %w", err)
	}
	return value, nil
}

// VerifyRFC9530ContentDigestSHA256 validates the sha-256 member of one or more
// Content-Digest field lines against the exact message content bytes.
func VerifyRFC9530ContentDigestSHA256(values []string, body []byte) error {
	if len(values) == 0 {
		return errors.New("Content-Digest field is missing")
	}

	dictionary, err := httpsfv.UnmarshalDictionary(values)
	if err != nil {
		return fmt.Errorf("parse Content-Digest structured field: %w", err)
	}

	member, ok := dictionary.Get("sha-256")
	if !ok {
		return errors.New("Content-Digest has no sha-256 member")
	}

	item, ok := member.(httpsfv.Item)
	if !ok {
		return errors.New("Content-Digest sha-256 member is not an item")
	}

	presented, ok := item.Value.([]byte)
	if !ok {
		return errors.New("Content-Digest sha-256 member is not a byte sequence")
	}

	expected := sha256.Sum256(body)
	if len(presented) != len(expected) ||
		subtle.ConstantTimeCompare(presented, expected[:]) != 1 {
		return errors.New("Content-Digest sha-256 value does not match message content")
	}

	return nil
}

// SignGETWithProfile signs an authorized-fetch GET using the selected profile.
// Existing SignGET call sites continue to use the legacy profile.
func (signer *Signer) SignGETWithProfile(
	request *http.Request,
	profile Profile,
) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	switch profile {
	case ProfileLegacy:
		return signer.SignGET(request)
	case ProfileRFC9421:
		return signer.signRFC9421(request, nil, false, rfc9421GETComponents)
	case ProfileDual:
		return ErrDualProfileRequiresDeliveryPolicy
	default:
		return fmt.Errorf("unsupported HTTP signature profile %q", profile)
	}
}

// SignPOSTWithProfile signs an ActivityPub delivery POST using the selected
// profile. Existing SignPOST call sites continue to use the legacy profile.
func (signer *Signer) SignPOSTWithProfile(
	request *http.Request,
	body []byte,
	profile Profile,
) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	switch profile {
	case ProfileLegacy:
		return signer.SignPOST(request, body)
	case ProfileRFC9421:
		return signer.signRFC9421(request, body, true, rfc9421POSTComponents)
	case ProfileDual:
		return ErrDualProfileRequiresDeliveryPolicy
	default:
		return fmt.Errorf("unsupported HTTP signature profile %q", profile)
	}
}

func (signer *Signer) signRFC9421(
	request *http.Request,
	body []byte,
	includeContentDigest bool,
	coveredComponents []string,
) error {
	if signer == nil ||
		signer.privateKey == nil ||
		strings.TrimSpace(signer.keyID) == "" {
		return errors.New("HTTP signature signer is not initialized")
	}
	if request == nil || request.URL == nil || request.URL.Host == "" {
		return errors.New("signed request has no URL host")
	}
	// URI fragments are client-side identifiers and are never part of the
	// HTTP request target. Remove them before deriving RFC 9421 components so
	// @target-uri matches the exact fragment-free URI seen by the receiver.
	request.URL.Fragment = ""
	request.URL.RawFragment = ""
	if request.Header == nil {
		request.Header = make(http.Header)
	}

	// Match the exact authority placed on the wire, including a non-default
	// port, just as the established legacy signer does.
	request.Host = request.URL.Host
	request.Header.Set("Host", request.Host)

	if request.Header.Get("Date") == "" {
		request.Header.Set("Date", httpdate.Time2Str(time.Now()))
	}

	// The legacy and RFC 9421 Signature fields have incompatible grammars.
	// A primitive signs exactly one profile and never emits an ambiguous mix.
	request.Header.Del("Signature")
	request.Header.Del("Signature-Input")
	request.Header.Del("Digest")

	if includeContentDigest {
		digest, err := RFC9530ContentDigestSHA256(body)
		if err != nil {
			return err
		}
		request.Header.Set("Content-Digest", digest)
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	} else {
		request.Header.Del("Content-Digest")
	}

	transport := &rfc9421signer.Transport{
		KeyID:             signer.keyID,
		Tag:               rfc9421SignatureTag,
		Alg:               alg_rsa.NewRSAPKCS256Signer(signer.privateKey),
		CoveredComponents: append([]string(nil), coveredComponents...),
	}

	message, err := transport.Sign(request)
	if err != nil {
		return fmt.Errorf("sign RFC 9421 request: %w", err)
	}

	set := &sigset.Set{
		Messages: map[string]*signature.Message{
			rfc9421SignatureLabel: message,
		},
	}
	if err := set.Include(request); err != nil {
		return fmt.Errorf("include RFC 9421 signature fields: %w", err)
	}

	return nil
}
