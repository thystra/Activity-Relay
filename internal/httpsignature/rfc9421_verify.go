package httpsignature

import (
	"bytes"
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/common-fate/httpsig/alg_rsa"
	"github.com/common-fate/httpsig/contentdigest"
	"github.com/common-fate/httpsig/sigbase"
	"github.com/common-fate/httpsig/sigset"
)

const (
	DefaultRFC9421MaximumAge = 5 * time.Minute
	DefaultRFC9421FutureSkew = 30 * time.Second
	DefaultRFC9421ReplayTTL  = 10 * time.Minute
	maxRFC9421NonceBytes     = 256
)

var ErrRFC9421Replay = errors.New("RFC 9421 signature nonce has already been used")

// RFC9421ResolvedKey is the verified key material and actor identity returned
// by a caller-supplied ActivityPub key resolver.
type RFC9421ResolvedKey struct {
	KeyID     string
	Owner     string
	ActorID   string
	PublicKey *rsa.PublicKey
}

// RFC9421KeyResolver resolves one RFC 9421 key ID to ActivityPub-owned RSA key
// material. Runtime wiring is responsible for authenticated remote retrieval.
type RFC9421KeyResolver interface {
	ResolveRFC9421Key(
		context.Context,
		string,
	) (RFC9421ResolvedKey, error)
}

// RFC9421KeyResolverFunc adapts a function to RFC9421KeyResolver.
type RFC9421KeyResolverFunc func(
	context.Context,
	string,
) (RFC9421ResolvedKey, error)

func (resolver RFC9421KeyResolverFunc) ResolveRFC9421Key(
	ctx context.Context,
	keyID string,
) (RFC9421ResolvedKey, error) {
	return resolver(ctx, keyID)
}

// RFC9421NonceStore atomically reserves a nonce for a key identity.
type RFC9421NonceStore interface {
	ReserveRFC9421Nonce(
		context.Context,
		string,
		string,
		time.Duration,
	) (bool, error)
}

// RFC9421VerifierOptions defines the fixed application verification policy.
type RFC9421VerifierOptions struct {
	Scheme      string
	Authority   string
	KeyResolver RFC9421KeyResolver
	NonceStore  RFC9421NonceStore
	MaximumAge  time.Duration
	FutureSkew  time.Duration
	ReplayTTL   time.Duration
	Now         func() time.Time
}

// RFC9421Verifier verifies the Activity-Relay RFC 9421 POST profile.
type RFC9421Verifier struct {
	scheme      string
	authority   string
	keyResolver RFC9421KeyResolver
	nonceStore  RFC9421NonceStore
	maximumAge  time.Duration
	futureSkew  time.Duration
	replayTTL   time.Duration
	now         func() time.Time
}

// RFC9421Verification contains identity and signature metadata established by
// successful cryptographic verification.
type RFC9421Verification struct {
	KeyID              string
	KeyOwner           string
	KeyActor           string
	Nonce              string
	Created            time.Time
	Expires            time.Time
	CoveredComponents  []string
	SignatureAlgorithm string
}

// NewRFC9421Verifier validates and creates an inbound verifier.
func NewRFC9421Verifier(
	options RFC9421VerifierOptions,
) (*RFC9421Verifier, error) {
	scheme := strings.ToLower(strings.TrimSpace(options.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("RFC 9421 verifier scheme %q is unsupported", scheme)
	}
	authority := strings.TrimSpace(options.Authority)
	if authority == "" {
		return nil, errors.New("RFC 9421 verifier authority is empty")
	}
	if options.KeyResolver == nil {
		return nil, errors.New("RFC 9421 key resolver is nil")
	}
	if options.NonceStore == nil {
		return nil, errors.New("RFC 9421 nonce store is nil")
	}

	maximumAge := options.MaximumAge
	if maximumAge == 0 {
		maximumAge = DefaultRFC9421MaximumAge
	}
	if maximumAge < time.Second {
		return nil, errors.New("RFC 9421 maximum age must be at least one second")
	}

	futureSkew := options.FutureSkew
	if futureSkew == 0 {
		futureSkew = DefaultRFC9421FutureSkew
	}
	if futureSkew < 0 {
		return nil, errors.New("RFC 9421 future skew cannot be negative")
	}

	replayTTL := options.ReplayTTL
	if replayTTL == 0 {
		replayTTL = DefaultRFC9421ReplayTTL
	}
	if replayTTL < maximumAge+futureSkew {
		return nil, errors.New(
			"RFC 9421 replay TTL must cover maximum age and future skew",
		)
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &RFC9421Verifier{
		scheme:      scheme,
		authority:   authority,
		keyResolver: options.KeyResolver,
		nonceStore:  options.NonceStore,
		maximumAge:  maximumAge,
		futureSkew:  futureSkew,
		replayTTL:   replayTTL,
		now:         now,
	}, nil
}

func requiredRFC9421POSTComponents() map[string]struct{} {
	required := make(map[string]struct{}, len(rfc9421POSTComponents))
	for _, component := range rfc9421POSTComponents {
		required[component] = struct{}{}
	}
	return required
}

func validateRFC9421POSTParameters(
	covered []string,
	keyID string,
	algorithm string,
	nonce string,
	created time.Time,
	expires time.Time,
	now time.Time,
	maximumAge time.Duration,
	futureSkew time.Duration,
) error {
	if strings.TrimSpace(keyID) == "" {
		return errors.New("RFC 9421 keyid parameter is required")
	}
	if algorithm != alg_rsa.RSASSA_PKCS1_1_5_SHA256 {
		return fmt.Errorf("RFC 9421 algorithm %q is not allowed", algorithm)
	}
	if nonce == "" {
		return errors.New("RFC 9421 nonce parameter is required")
	}
	if len(nonce) > maxRFC9421NonceBytes {
		return errors.New("RFC 9421 nonce exceeds 256 bytes")
	}
	if created.IsZero() {
		return errors.New("RFC 9421 created parameter is required")
	}
	if created.Before(now.Add(-maximumAge)) {
		return errors.New("RFC 9421 signature is older than the allowed maximum age")
	}
	if created.After(now.Add(futureSkew)) {
		return errors.New("RFC 9421 signature was created too far in the future")
	}
	if !expires.IsZero() {
		if expires.Before(created) {
			return errors.New("RFC 9421 expires precedes created")
		}
		if expires.Before(now) {
			return errors.New("RFC 9421 signature has expired")
		}
	}

	required := requiredRFC9421POSTComponents()
	seen := make(map[string]struct{}, len(covered))
	for _, component := range covered {
		if component == "" {
			return errors.New("RFC 9421 covered component is empty")
		}
		if _, duplicate := seen[component]; duplicate {
			return fmt.Errorf(
				"RFC 9421 covered component %q is duplicated",
				component,
			)
		}
		seen[component] = struct{}{}
		delete(required, component)
	}
	if len(required) != 0 {
		for _, component := range rfc9421POSTComponents {
			if _, missing := required[component]; missing {
				return fmt.Errorf(
					"RFC 9421 required covered component %q is missing",
					component,
				)
			}
		}
	}

	return nil
}

func cloneRFC9421Request(
	request *http.Request,
	body []byte,
	scheme string,
	authority string,
) *http.Request {
	cloned := request.Clone(request.Context())
	cloned.Header = request.Header.Clone()
	if request.URL != nil {
		copiedURL := *request.URL
		cloned.URL = &copiedURL
	}
	cloned.URL.Scheme = scheme
	cloned.URL.Host = authority
	cloned.Host = authority
	cloned.Body = io.NopCloser(bytes.NewReader(body))
	cloned.ContentLength = int64(len(body))
	cloned.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return cloned
}

// VerifyPOST verifies one RFC 9421 ActivityPub POST and reserves its nonce only
// after the signature and exact body digest have both succeeded.
func (verifier *RFC9421Verifier) VerifyPOST(
	request *http.Request,
	body []byte,
) (*RFC9421Verification, error) {
	if verifier == nil {
		return nil, errors.New("RFC 9421 verifier is nil")
	}
	if request == nil || request.URL == nil {
		return nil, errors.New("RFC 9421 request or URL is nil")
	}
	if request.Method != http.MethodPost {
		return nil, fmt.Errorf(
			"RFC 9421 method %q is not allowed for ActivityPub delivery",
			request.Method,
		)
	}

	wireAuthority := request.Host
	if wireAuthority == "" {
		wireAuthority = request.URL.Host
	}
	if wireAuthority != verifier.authority {
		return nil, fmt.Errorf(
			"RFC 9421 request authority %q does not match expected %q",
			wireAuthority,
			verifier.authority,
		)
	}

	set, err := sigset.Unmarshal(request)
	if err != nil {
		return nil, fmt.Errorf("parse RFC 9421 signature fields: %w", err)
	}
	message, err := set.Find(rfc9421SignatureTag)
	if err != nil {
		return nil, fmt.Errorf("find ActivityPub RFC 9421 signature: %w", err)
	}

	now := verifier.now().UTC()
	if err := validateRFC9421POSTParameters(
		message.Input.CoveredComponents,
		message.Input.KeyID,
		message.Input.Alg,
		message.Input.Nonce,
		message.Input.Created,
		message.Input.Expires,
		now,
		verifier.maximumAge,
		verifier.futureSkew,
	); err != nil {
		return nil, err
	}

	if err := VerifyRFC9530ContentDigestSHA256(
		request.Header.Values("Content-Digest"),
		body,
	); err != nil {
		return nil, err
	}

	resolved, err := verifier.keyResolver.ResolveRFC9421Key(
		request.Context(),
		message.Input.KeyID,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve RFC 9421 key: %w", err)
	}
	if resolved.KeyID != message.Input.KeyID {
		return nil, errors.New("resolved RFC 9421 key ID does not match keyid")
	}
	if resolved.PublicKey == nil {
		return nil, errors.New("resolved RFC 9421 RSA public key is nil")
	}

	wireRequest := cloneRFC9421Request(
		request,
		body,
		verifier.scheme,
		verifier.authority,
	)
	base, err := sigbase.Derive(
		message.Input,
		nil,
		wireRequest,
		contentdigest.SHA256,
	)
	if err != nil {
		return nil, fmt.Errorf("derive RFC 9421 signature base: %w", err)
	}
	signingString, err := base.CanonicalString(message.Input)
	if err != nil {
		return nil, fmt.Errorf("serialize RFC 9421 signature base: %w", err)
	}
	if err := alg_rsa.NewRSAPKCS256Verifier(resolved.PublicKey).Verify(
		request.Context(),
		signingString,
		message.Signature,
	); err != nil {
		return nil, fmt.Errorf("verify RFC 9421 signature: %w", err)
	}

	reserved, err := verifier.nonceStore.ReserveRFC9421Nonce(
		request.Context(),
		message.Input.KeyID,
		message.Input.Nonce,
		verifier.replayTTL,
	)
	if err != nil {
		return nil, fmt.Errorf("reserve RFC 9421 nonce: %w", err)
	}
	if !reserved {
		return nil, ErrRFC9421Replay
	}

	return &RFC9421Verification{
		KeyID:    message.Input.KeyID,
		KeyOwner: resolved.Owner,
		KeyActor: resolved.ActorID,
		Nonce:    message.Input.Nonce,
		Created:  message.Input.Created,
		Expires:  message.Input.Expires,
		CoveredComponents: append(
			[]string(nil),
			message.Input.CoveredComponents...,
		),
		SignatureAlgorithm: message.Input.Alg,
	}, nil
}

func canonicalRFC9421ActorIdentity(value string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("actor identity URL is not absolute")
	}
	if parsed.User != nil {
		return "", errors.New("actor identity URL contains user information")
	}
	if parsed.Fragment != "" {
		return "", errors.New("actor identity URL contains a fragment")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

// BindActivityActor requires the verified public-key owner and resolved actor
// document identity to identify the ActivityPub activity actor.
func (verification *RFC9421Verification) BindActivityActor(
	activityActor string,
) error {
	if verification == nil {
		return errors.New("RFC 9421 verification result is nil")
	}

	activityIdentity, err := canonicalRFC9421ActorIdentity(activityActor)
	if err != nil {
		return fmt.Errorf("invalid activity actor identity: %w", err)
	}
	ownerIdentity, err := canonicalRFC9421ActorIdentity(verification.KeyOwner)
	if err != nil {
		return fmt.Errorf("invalid public-key owner identity: %w", err)
	}
	if ownerIdentity != activityIdentity {
		return errors.New(
			"RFC 9421 public-key owner does not match activity actor",
		)
	}

	if strings.TrimSpace(verification.KeyActor) != "" {
		keyActorIdentity, err := canonicalRFC9421ActorIdentity(
			verification.KeyActor,
		)
		if err != nil {
			return fmt.Errorf("invalid resolved key actor identity: %w", err)
		}
		if keyActorIdentity != activityIdentity {
			return errors.New(
				"RFC 9421 resolved key actor does not match activity actor",
			)
		}
	}

	return nil
}
