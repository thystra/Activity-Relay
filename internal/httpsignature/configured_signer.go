package httpsignature

import (
	"crypto/rsa"
	"fmt"
	"net/http"
)

// ParseOutboundProfile validates one operator-selected outbound profile.
// Empty input preserves the established legacy behavior. Dual remains a
// destination negotiation policy and is rejected as a process-wide wire mode.
func ParseOutboundProfile(value string) (Profile, error) {
	profile, err := ParseProfile(value)
	if err != nil {
		return "", err
	}
	if profile == ProfileDual {
		return "", ErrDualProfileRequiresDeliveryPolicy
	}
	return profile, nil
}

// ConfiguredSigner applies one validated outbound profile to authorized-fetch
// GETs and ActivityPub delivery POSTs.
type ConfiguredSigner struct {
	signer  *Signer
	profile Profile
}

// NewConfiguredSigner creates a signer for one process-wide outbound profile.
func NewConfiguredSigner(
	keyID string,
	privateKey *rsa.PrivateKey,
	profile Profile,
) (*ConfiguredSigner, error) {
	normalized, err := ParseOutboundProfile(profile.String())
	if err != nil {
		return nil, fmt.Errorf("outbound HTTP signature profile: %w", err)
	}
	signer, err := NewSigner(keyID, privateKey)
	if err != nil {
		return nil, err
	}
	return &ConfiguredSigner{
		signer:  signer,
		profile: normalized,
	}, nil
}

// Profile returns the normalized configured profile.
func (signer *ConfiguredSigner) Profile() Profile {
	if signer == nil {
		return ""
	}
	return signer.profile
}

// SignGET signs one authorized-fetch request using the configured profile.
func (signer *ConfiguredSigner) SignGET(request *http.Request) error {
	if signer == nil || signer.signer == nil {
		return fmt.Errorf("configured outbound HTTP signer is not initialized")
	}
	return signer.signer.SignGETWithProfile(request, signer.profile)
}

// SignPOST signs one delivery request using the configured profile.
func (signer *ConfiguredSigner) SignPOST(
	request *http.Request,
	body []byte,
) error {
	if signer == nil || signer.signer == nil {
		return fmt.Errorf("configured outbound HTTP signer is not initialized")
	}
	return signer.signer.SignPOSTWithProfile(request, body, signer.profile)
}
