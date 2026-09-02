package httpsignature

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

// ParseOutboundProfile validates one operator-selected outbound policy.
// Empty input selects the 3.0 destination-aware compatibility default.
func ParseOutboundProfile(value string) (Profile, error) {
	if strings.TrimSpace(value) == "" {
		return ProfileDual, nil
	}
	return ParseProfile(value)
}

// ParseWireProfile accepts only a concrete single-request wire profile.
func ParseWireProfile(value string) (Profile, error) {
	profile, err := ParseProfile(value)
	if err != nil {
		return "", err
	}
	if profile == ProfileDual {
		return "", ErrDualProfileRequiresDeliveryPolicy
	}
	return profile, nil
}

// ConfiguredSigner applies a fixed or destination-negotiated outbound policy.
type ConfiguredSigner struct {
	signer     *Signer
	profile    Profile
	negotiator *DestinationNegotiator
}

// NewConfiguredSigner preserves the fixed-profile constructor. Dual requires
// the explicit negotiating constructor.
func NewConfiguredSigner(
	keyID string,
	privateKey *rsa.PrivateKey,
	profile Profile,
) (*ConfiguredSigner, error) {
	normalized, err := ParseProfile(profile.String())
	if err != nil {
		return nil, fmt.Errorf(
			"outbound HTTP signature profile: %w",
			err,
		)
	}
	if normalized == ProfileDual {
		return nil, ErrDualProfileRequiresDeliveryPolicy
	}
	return newConfiguredSigner(
		keyID,
		privateKey,
		normalized,
		nil,
	)
}

// NewNegotiatingSigner creates the runtime signer. Dual is accepted only when
// the reviewed destination negotiator is available.
func NewNegotiatingSigner(
	keyID string,
	privateKey *rsa.PrivateKey,
	profile Profile,
	negotiator *DestinationNegotiator,
) (*ConfiguredSigner, error) {
	normalized, err := ParseOutboundProfile(profile.String())
	if err != nil {
		return nil, fmt.Errorf(
			"outbound HTTP signature profile: %w",
			err,
		)
	}
	if normalized == ProfileDual && negotiator == nil {
		return nil, ErrDualProfileRequiresDeliveryPolicy
	}
	return newConfiguredSigner(
		keyID,
		privateKey,
		normalized,
		negotiator,
	)
}

func newConfiguredSigner(
	keyID string,
	privateKey *rsa.PrivateKey,
	profile Profile,
	negotiator *DestinationNegotiator,
) (*ConfiguredSigner, error) {
	signer, err := NewSigner(keyID, privateKey)
	if err != nil {
		return nil, err
	}
	return &ConfiguredSigner{
		signer:     signer,
		profile:    profile,
		negotiator: negotiator,
	}, nil
}

// Profile returns the normalized configured policy.
func (signer *ConfiguredSigner) Profile() Profile {
	if signer == nil {
		return ""
	}
	return signer.profile
}

// Plan chooses one concrete wire profile for a destination and operation.
func (signer *ConfiguredSigner) Plan(
	ctx context.Context,
	scope DestinationScope,
	destination string,
) (NegotiationPlan, error) {
	if signer == nil || signer.signer == nil {
		return NegotiationPlan{}, errors.New(
			"configured outbound HTTP signer is not initialized",
		)
	}
	if signer.negotiator != nil {
		return signer.negotiator.Plan(
			ctx,
			signer.profile,
			scope,
			destination,
		)
	}
	if signer.profile == ProfileDual {
		return NegotiationPlan{}, ErrDualProfileRequiresDeliveryPolicy
	}
	origin, err := NormalizeDestinationOrigin(destination)
	if err != nil {
		return NegotiationPlan{}, err
	}
	reason := NegotiationReasonConfiguredLegacy
	if signer.profile == ProfileRFC9421 {
		reason = NegotiationReasonConfiguredRFC9421
	}
	return NegotiationPlan{
		Origin:  origin,
		Scope:   scope,
		Primary: signer.profile,
		Reason:  reason,
	}, nil
}

// SignGETWithProfile signs one GET with a concrete wire profile.
func (signer *ConfiguredSigner) SignGETWithProfile(
	request *http.Request,
	profile Profile,
) error {
	if signer == nil || signer.signer == nil {
		return errors.New(
			"configured outbound HTTP signer is not initialized",
		)
	}
	wireProfile, err := ParseWireProfile(profile.String())
	if err != nil {
		return err
	}
	return signer.signer.SignGETWithProfile(request, wireProfile)
}

// SignPOSTWithProfile signs one POST with a concrete wire profile.
func (signer *ConfiguredSigner) SignPOSTWithProfile(
	request *http.Request,
	body []byte,
	profile Profile,
) error {
	if signer == nil || signer.signer == nil {
		return errors.New(
			"configured outbound HTTP signer is not initialized",
		)
	}
	wireProfile, err := ParseWireProfile(profile.String())
	if err != nil {
		return err
	}
	return signer.signer.SignPOSTWithProfile(
		request,
		body,
		wireProfile,
	)
}

// SignGET signs one authorized-fetch request using the configured plan's
// primary profile. DoGET must be used when a bounded fallback is desired.
func (signer *ConfiguredSigner) SignGET(request *http.Request) error {
	if request == nil || request.URL == nil {
		return errors.New("signed GET request has no URL")
	}
	plan, err := signer.Plan(
		request.Context(),
		DestinationScopeFetch,
		request.URL.String(),
	)
	if err != nil {
		return err
	}
	return signer.SignGETWithProfile(request, plan.Primary)
}

// SignPOST signs one delivery request using the configured plan's primary
// profile. Runtime delivery uses an explicit queued wire profile instead.
func (signer *ConfiguredSigner) SignPOST(
	request *http.Request,
	body []byte,
) error {
	if request == nil || request.URL == nil {
		return errors.New("signed POST request has no URL")
	}
	plan, err := signer.Plan(
		request.Context(),
		DestinationScopeDelivery,
		request.URL.String(),
	)
	if err != nil {
		return err
	}
	return signer.SignPOSTWithProfile(
		request,
		body,
		plan.Primary,
	)
}

type requestNegotiationState struct {
	plan NegotiationPlan
}

type requestNegotiationContextKey struct{}

func clearSignatureFields(request *http.Request) {
	request.Header.Del("Date")
	request.Header.Del("Digest")
	request.Header.Del("Content-Digest")
	request.Header.Del("Signature-Input")
	request.Header.Del("Signature")
}

func attachRequestPlan(
	request *http.Request,
	plan NegotiationPlan,
) {
	contextWithPlan := context.WithValue(
		request.Context(),
		requestNegotiationContextKey{},
		requestNegotiationState{plan: plan},
	)
	*request = *request.WithContext(contextWithPlan)
}

func requestPlan(
	request *http.Request,
) (NegotiationPlan, bool) {
	if request == nil {
		return NegotiationPlan{}, false
	}
	state, ok := request.Context().Value(
		requestNegotiationContextKey{},
	).(requestNegotiationState)
	return state.plan, ok
}

func (signer *ConfiguredSigner) signPlannedGET(
	request *http.Request,
) (NegotiationPlan, error) {
	plan, err := signer.Plan(
		request.Context(),
		DestinationScopeFetch,
		request.URL.String(),
	)
	if err != nil {
		return NegotiationPlan{}, err
	}
	clearSignatureFields(request)
	if err := signer.SignGETWithProfile(
		request,
		plan.Primary,
	); err != nil {
		return NegotiationPlan{}, err
	}
	attachRequestPlan(request, plan)
	return plan, nil
}

func (signer *ConfiguredSigner) observeBestEffort(
	ctx context.Context,
	scope DestinationScope,
	destination string,
	profile Profile,
	statusCode int,
	header http.Header,
) {
	if err := signer.ObserveResponse(
		ctx,
		scope,
		destination,
		profile,
		statusCode,
		header,
	); err != nil {
		logrus.WithError(err).
			WithField("destination", destination).
			Debug("Unable to store HTTP signature capability evidence")
	}
}

func (signer *ConfiguredSigner) observeLegacyFallbackBestEffort(
	ctx context.Context,
	destination string,
) {
	if signer == nil || signer.negotiator == nil {
		return
	}
	_, _, err := signer.negotiator.ObserveSuccessfulLegacyFallback(
		ctx,
		DestinationScopeFetch,
		destination,
	)
	if err != nil {
		logrus.WithError(err).
			WithField("destination", destination).
			Debug("Unable to store successful legacy fallback evidence")
	}
}

// DoGET executes one signed GET. An unknown dual destination may make one
// legacy fallback after an explicit legacy Signature challenge or the bounded
// HTTP 400 compatibility signal.
func (signer *ConfiguredSigner) DoGET(
	client *http.Client,
	request *http.Request,
) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("remote HTTP client is nil")
	}
	if request == nil || request.URL == nil {
		return nil, errors.New("remote GET request has no URL")
	}
	callerContext := request.Context()

	initialPlan, err := signer.signPlannedGET(request)
	if err != nil {
		return nil, fmt.Errorf("sign remote request: %w", err)
	}

	clientCopy := *client
	previousRedirect := client.CheckRedirect
	clientCopy.CheckRedirect = func(
		redirect *http.Request,
		via []*http.Request,
	) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if previousRedirect != nil {
			if err := previousRedirect(redirect, via); err != nil {
				return err
			}
		}
		if _, err := signer.signPlannedGET(redirect); err != nil {
			return fmt.Errorf(
				"sign redirected remote request: %w",
				err,
			)
		}
		return nil
	}

	response, err := clientCopy.Do(request)
	if err != nil {
		return nil, err
	}
	finalPlan := initialPlan
	if plan, ok := requestPlan(response.Request); ok {
		finalPlan = plan
	}
	signer.observeBestEffort(
		request.Context(),
		DestinationScopeFetch,
		response.Request.URL.String(),
		finalPlan.Primary,
		response.StatusCode,
		response.Header,
	)

	outcome := classifyNegotiationOutcome(response)
	if !finalPlan.ShouldFallback(outcome) {
		return response, nil
	}

	_, _ = io.Copy(
		io.Discard,
		io.LimitReader(response.Body, 4096),
	)
	_ = response.Body.Close()

	fallbackRequest := response.Request.Clone(
		callerContext,
	)
	clearSignatureFields(fallbackRequest)
	fallbackPlan := NegotiationPlan{
		Origin:  finalPlan.Origin,
		Scope:   DestinationScopeFetch,
		Primary: finalPlan.Fallback,
		Reason:  NegotiationReasonCachedCapability,
	}
	if err := signer.SignGETWithProfile(
		fallbackRequest,
		fallbackPlan.Primary,
	); err != nil {
		return nil, fmt.Errorf(
			"sign legacy fallback remote request: %w",
			err,
		)
	}
	attachRequestPlan(fallbackRequest, fallbackPlan)

	fallbackResponse, err := clientCopy.Do(fallbackRequest)
	if err != nil {
		return nil, err
	}
	appliedPlan := fallbackPlan
	if plan, ok := requestPlan(fallbackResponse.Request); ok {
		appliedPlan = plan
	}
	signer.observeBestEffort(
		fallbackRequest.Context(),
		DestinationScopeFetch,
		fallbackResponse.Request.URL.String(),
		appliedPlan.Primary,
		fallbackResponse.StatusCode,
		fallbackResponse.Header,
	)
	if outcome == NegotiationOutcomeAmbiguousClientRejection &&
		appliedPlan.Primary == ProfileLegacy &&
		fallbackResponse.StatusCode/100 == 2 {
		signer.observeLegacyFallbackBestEffort(
			fallbackRequest.Context(),
			fallbackResponse.Request.URL.String(),
		)
	}
	return fallbackResponse, nil
}

func classifyNegotiationOutcome(
	response *http.Response,
) NegotiationOutcome {
	if response == nil {
		return NegotiationOutcomeTransportFailure
	}
	if response.StatusCode/100 == 2 {
		return NegotiationOutcomeSuccess
	}
	if ExplicitLegacySignatureRejection(
		response.StatusCode,
		response.Header,
	) {
		return NegotiationOutcomeExplicitSignatureRejection
	}
	if response.StatusCode == http.StatusBadRequest {
		return NegotiationOutcomeAmbiguousClientRejection
	}
	if response.StatusCode/100 == 5 {
		return NegotiationOutcomeServerFailure
	}
	return NegotiationOutcomeOtherFailure
}

// ObserveResponse records only bounded, machine-readable evidence.
func (signer *ConfiguredSigner) ObserveResponse(
	ctx context.Context,
	scope DestinationScope,
	destination string,
	profile Profile,
	statusCode int,
	header http.Header,
) error {
	if signer == nil || signer.negotiator == nil {
		return nil
	}
	if statusCode/100 == 2 {
		evidence := CapabilityEvidenceAcceptSignature
		if profile == ProfileRFC9421 {
			evidence = CapabilityEvidenceSuccessfulRFC9421
		} else if !CompatibleAcceptSignature(
			header.Values("Accept-Signature"),
			scope,
			signer.signer.keyID,
		) {
			return nil
		}
		_, _, err := signer.negotiator.ObserveRFC9421Success(
			ctx,
			scope,
			destination,
			evidence,
		)
		return err
	}
	if profile == ProfileRFC9421 &&
		ExplicitLegacySignatureRejection(statusCode, header) {
		_, _, err :=
			signer.negotiator.ObserveExplicitRFC9421Rejection(
				ctx,
				scope,
				destination,
			)
		return err
	}
	return nil
}
