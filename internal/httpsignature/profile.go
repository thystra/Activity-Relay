package httpsignature

import (
	"errors"
	"fmt"
	"strings"
)

// Profile identifies an outbound HTTP-signature policy.
//
// Dual is a delivery negotiation policy rather than a wire format. A single
// request cannot safely contain both the established Fediverse Signature field
// syntax and RFC 9421 Signature field syntax.
type Profile string

const (
	ProfileLegacy  Profile = "legacy"
	ProfileDual    Profile = "dual"
	ProfileRFC9421 Profile = "rfc9421"
)

var ErrDualProfileRequiresDeliveryPolicy = errors.New(
	"dual HTTP signature profile requires destination-aware delivery policy",
)

// ParseProfile validates a configured profile. An empty value preserves the
// established legacy behavior.
func ParseProfile(value string) (Profile, error) {
	profile := Profile(strings.ToLower(strings.TrimSpace(value)))
	if profile == "" {
		return ProfileLegacy, nil
	}
	if err := profile.Validate(); err != nil {
		return "", err
	}
	return profile, nil
}

// Validate reports whether a profile name is supported.
func (profile Profile) Validate() error {
	switch profile {
	case ProfileLegacy, ProfileDual, ProfileRFC9421:
		return nil
	default:
		return fmt.Errorf("unsupported HTTP signature profile %q", profile)
	}
}

func (profile Profile) String() string {
	return string(profile)
}
