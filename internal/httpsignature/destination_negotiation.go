package httpsignature

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultRFC9421CapabilityTTL = 14 * 24 * time.Hour
	DefaultLegacyNegativeTTL    = 24 * time.Hour
)

// DestinationScope separates idempotent remote fetch behavior from
// non-idempotent ActivityPub delivery behavior.
type DestinationScope string

const (
	DestinationScopeFetch    DestinationScope = "fetch"
	DestinationScopeDelivery DestinationScope = "delivery"
)

func (scope DestinationScope) Validate() error {
	switch scope {
	case DestinationScopeFetch, DestinationScopeDelivery:
		return nil
	default:
		return fmt.Errorf(
			"unsupported HTTP signature destination scope %q",
			scope,
		)
	}
}

// CapabilityEvidence identifies the bounded event that produced one cached
// destination preference.
type CapabilityEvidence string

const (
	CapabilityEvidenceSuccessfulRFC9421        CapabilityEvidence = "successful_rfc9421"
	CapabilityEvidenceAcceptSignature          CapabilityEvidence = "accept_signature"
	CapabilityEvidenceExplicitRFC9421Rejection CapabilityEvidence = "explicit_rfc9421_rejection"
)

func (evidence CapabilityEvidence) Validate(profile Profile) error {
	switch evidence {
	case CapabilityEvidenceSuccessfulRFC9421,
		CapabilityEvidenceAcceptSignature:
		if profile != ProfileRFC9421 {
			return fmt.Errorf(
				"capability evidence %q requires profile %q",
				evidence,
				ProfileRFC9421,
			)
		}
		return nil
	case CapabilityEvidenceExplicitRFC9421Rejection:
		if profile != ProfileLegacy {
			return fmt.Errorf(
				"capability evidence %q requires profile %q",
				evidence,
				ProfileLegacy,
			)
		}
		return nil
	default:
		return fmt.Errorf(
			"unsupported HTTP signature capability evidence %q",
			evidence,
		)
	}
}

// DestinationCapability is one expiring origin-scoped negotiation decision.
type DestinationCapability struct {
	Origin     string
	Scope      DestinationScope
	Profile    Profile
	Evidence   CapabilityEvidence
	ObservedAt time.Time
	ExpiresAt  time.Time
}

func (capability DestinationCapability) Validate() error {
	normalizedOrigin, err := NormalizeDestinationOrigin(capability.Origin)
	if err != nil {
		return err
	}
	if normalizedOrigin != capability.Origin {
		return errors.New(
			"destination capability origin is not normalized",
		)
	}
	if err := capability.Scope.Validate(); err != nil {
		return err
	}
	switch capability.Profile {
	case ProfileLegacy, ProfileRFC9421:
	default:
		return fmt.Errorf(
			"destination capability profile %q is not a wire profile",
			capability.Profile,
		)
	}
	if err := capability.Evidence.Validate(capability.Profile); err != nil {
		return err
	}
	if capability.ObservedAt.IsZero() {
		return errors.New(
			"destination capability observation time is empty",
		)
	}
	if !capability.ExpiresAt.After(capability.ObservedAt) {
		return errors.New(
			"destination capability expiration must follow observation",
		)
	}
	return nil
}

func (capability DestinationCapability) Fresh(now time.Time) bool {
	if now.IsZero() {
		now = time.Now()
	}
	return capability.ExpiresAt.After(now)
}

// NormalizeDestinationOrigin returns a lower-case HTTP(S) origin, stripping
// only the default port for its scheme. Paths, queries, and fragments are not
// part of destination capability identity.
func NormalizeDestinationOrigin(address string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return "", fmt.Errorf("parse destination URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf(
			"unsupported destination URL scheme %q",
			parsed.Scheme,
		)
	}
	if parsed.User != nil {
		return "", errors.New(
			"destination URL contains user information",
		)
	}

	host := strings.ToLower(
		strings.TrimSuffix(parsed.Hostname(), "."),
	)
	if host == "" {
		return "", errors.New("destination URL has no host")
	}
	port := parsed.Port()
	if (scheme == "http" && port == "80") ||
		(scheme == "https" && port == "443") {
		port = ""
	}

	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if port != "" {
		authority = net.JoinHostPort(host, port)
	}

	return scheme + "://" + authority, nil
}

// DestinationCapabilityStore persists origin- and scope-specific evidence.
type DestinationCapabilityStore interface {
	LoadDestinationCapability(
		context.Context,
		DestinationScope,
		string,
	) (DestinationCapability, bool, error)
	SaveDestinationCapability(
		context.Context,
		DestinationCapability,
	) (bool, error)
}

// NegotiationReason is a bounded explanation for one signature plan.
type NegotiationReason string

const (
	NegotiationReasonConfiguredLegacy      NegotiationReason = "configured_legacy"
	NegotiationReasonConfiguredRFC9421     NegotiationReason = "configured_rfc9421"
	NegotiationReasonCachedCapability      NegotiationReason = "cached_capability"
	NegotiationReasonUnknownFetchProbe     NegotiationReason = "unknown_fetch_probe"
	NegotiationReasonUnknownDeliveryLegacy NegotiationReason = "unknown_delivery_legacy"
)

// NegotiationOutcome is a bounded result used only to decide whether an
// idempotent GET may use its precomputed fallback.
type NegotiationOutcome string

const (
	NegotiationOutcomeSuccess                    NegotiationOutcome = "success"
	NegotiationOutcomeExplicitSignatureRejection NegotiationOutcome = "explicit_signature_rejection"
	NegotiationOutcomeTransportFailure           NegotiationOutcome = "transport_failure"
	NegotiationOutcomeServerFailure              NegotiationOutcome = "server_failure"
	NegotiationOutcomeOtherFailure               NegotiationOutcome = "other_failure"
)

// NegotiationPlan selects exactly one wire profile for the first attempt.
// Fallback is populated only for an unknown destination fetch.
type NegotiationPlan struct {
	Origin   string
	Scope    DestinationScope
	Primary  Profile
	Fallback Profile
	Reason   NegotiationReason
}

func (plan NegotiationPlan) HasFallback() bool {
	return plan.Fallback != ""
}

// ShouldFallback is intentionally narrow. It never permits POST fallback and
// never treats transport or generic HTTP failure as a signature negotiation
// signal.
func (plan NegotiationPlan) ShouldFallback(
	outcome NegotiationOutcome,
) bool {
	return plan.Scope == DestinationScopeFetch &&
		plan.Primary == ProfileRFC9421 &&
		plan.Fallback == ProfileLegacy &&
		outcome == NegotiationOutcomeExplicitSignatureRejection
}

// DestinationNegotiatorOptions configures the reusable planning core.
type DestinationNegotiatorOptions struct {
	Store       DestinationCapabilityStore
	PositiveTTL time.Duration
	NegativeTTL time.Duration
	Now         func() time.Time
}

// DestinationNegotiator plans wire profiles and records bounded observations.
type DestinationNegotiator struct {
	store       DestinationCapabilityStore
	positiveTTL time.Duration
	negativeTTL time.Duration
	now         func() time.Time
}

func NewDestinationNegotiator(
	options DestinationNegotiatorOptions,
) (*DestinationNegotiator, error) {
	positiveTTL := options.PositiveTTL
	if positiveTTL == 0 {
		positiveTTL = DefaultRFC9421CapabilityTTL
	}
	if positiveTTL < time.Hour {
		return nil, errors.New(
			"RFC 9421 positive capability TTL must be at least one hour",
		)
	}

	negativeTTL := options.NegativeTTL
	if negativeTTL == 0 {
		negativeTTL = DefaultLegacyNegativeTTL
	}
	if negativeTTL < time.Minute {
		return nil, errors.New(
			"legacy negative capability TTL must be at least one minute",
		)
	}
	if negativeTTL > positiveTTL {
		return nil, errors.New(
			"legacy negative capability TTL cannot exceed positive TTL",
		)
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &DestinationNegotiator{
		store:       options.Store,
		positiveTTL: positiveTTL,
		negativeTTL: negativeTTL,
		now:         now,
	}, nil
}

// Plan returns one deterministic wire-profile plan. Fixed legacy and RFC 9421
// modes do not consult capability state. Dual requires a store but remains a
// core-only policy until a later runtime tranche enables it.
func (negotiator *DestinationNegotiator) Plan(
	ctx context.Context,
	configured Profile,
	scope DestinationScope,
	destination string,
) (NegotiationPlan, error) {
	if negotiator == nil {
		return NegotiationPlan{}, errors.New(
			"destination negotiator is nil",
		)
	}
	if err := scope.Validate(); err != nil {
		return NegotiationPlan{}, err
	}
	origin, err := NormalizeDestinationOrigin(destination)
	if err != nil {
		return NegotiationPlan{}, err
	}

	switch configured {
	case ProfileLegacy:
		return NegotiationPlan{
			Origin:  origin,
			Scope:   scope,
			Primary: ProfileLegacy,
			Reason:  NegotiationReasonConfiguredLegacy,
		}, nil
	case ProfileRFC9421:
		return NegotiationPlan{
			Origin:  origin,
			Scope:   scope,
			Primary: ProfileRFC9421,
			Reason:  NegotiationReasonConfiguredRFC9421,
		}, nil
	case ProfileDual:
	default:
		return NegotiationPlan{}, fmt.Errorf(
			"unsupported configured HTTP signature profile %q",
			configured,
		)
	}

	if negotiator.store == nil {
		return NegotiationPlan{}, errors.New(
			"dual HTTP signature negotiation requires a capability store",
		)
	}

	capability, found, err :=
		negotiator.store.LoadDestinationCapability(
			ctx,
			scope,
			origin,
		)
	if err != nil {
		return NegotiationPlan{}, err
	}
	if found && capability.Fresh(negotiator.now().UTC()) {
		if err := capability.Validate(); err != nil {
			return NegotiationPlan{}, fmt.Errorf(
				"invalid cached destination capability: %w",
				err,
			)
		}
		return NegotiationPlan{
			Origin:  origin,
			Scope:   scope,
			Primary: capability.Profile,
			Reason:  NegotiationReasonCachedCapability,
		}, nil
	}

	if scope == DestinationScopeFetch {
		return NegotiationPlan{
			Origin:   origin,
			Scope:    scope,
			Primary:  ProfileRFC9421,
			Fallback: ProfileLegacy,
			Reason:   NegotiationReasonUnknownFetchProbe,
		}, nil
	}

	return NegotiationPlan{
		Origin:  origin,
		Scope:   scope,
		Primary: ProfileLegacy,
		Reason:  NegotiationReasonUnknownDeliveryLegacy,
	}, nil
}

func (negotiator *DestinationNegotiator) saveObservation(
	ctx context.Context,
	scope DestinationScope,
	destination string,
	profile Profile,
	evidence CapabilityEvidence,
	ttl time.Duration,
) (DestinationCapability, bool, error) {
	if negotiator == nil {
		return DestinationCapability{}, false, errors.New(
			"destination negotiator is nil",
		)
	}
	if negotiator.store == nil {
		return DestinationCapability{}, false, errors.New(
			"destination capability store is nil",
		)
	}
	if err := scope.Validate(); err != nil {
		return DestinationCapability{}, false, err
	}
	if err := evidence.Validate(profile); err != nil {
		return DestinationCapability{}, false, err
	}
	origin, err := NormalizeDestinationOrigin(destination)
	if err != nil {
		return DestinationCapability{}, false, err
	}
	observedAt := negotiator.now().UTC()
	capability := DestinationCapability{
		Origin:     origin,
		Scope:      scope,
		Profile:    profile,
		Evidence:   evidence,
		ObservedAt: observedAt,
		ExpiresAt:  observedAt.Add(ttl),
	}
	if err := capability.Validate(); err != nil {
		return DestinationCapability{}, false, err
	}
	saved, err := negotiator.store.SaveDestinationCapability(
		ctx,
		capability,
	)
	return capability, saved, err
}

// ObserveRFC9421Success records a successful modern request or an explicit
// Accept-Signature indication.
func (negotiator *DestinationNegotiator) ObserveRFC9421Success(
	ctx context.Context,
	scope DestinationScope,
	destination string,
	evidence CapabilityEvidence,
) (DestinationCapability, bool, error) {
	if evidence != CapabilityEvidenceSuccessfulRFC9421 &&
		evidence != CapabilityEvidenceAcceptSignature {
		return DestinationCapability{}, false, fmt.Errorf(
			"evidence %q is not positive RFC 9421 evidence",
			evidence,
		)
	}
	return negotiator.saveObservation(
		ctx,
		scope,
		destination,
		ProfileRFC9421,
		evidence,
		negotiator.positiveTTL,
	)
}

// ObserveExplicitRFC9421Rejection records a short-lived legacy preference only
// after a later runtime layer has explicitly classified a modern signature
// rejection. Generic status codes and transport failures are not evidence.
func (negotiator *DestinationNegotiator) ObserveExplicitRFC9421Rejection(
	ctx context.Context,
	scope DestinationScope,
	destination string,
) (DestinationCapability, bool, error) {
	return negotiator.saveObservation(
		ctx,
		scope,
		destination,
		ProfileLegacy,
		CapabilityEvidenceExplicitRFC9421Rejection,
		negotiator.negativeTTL,
	)
}
