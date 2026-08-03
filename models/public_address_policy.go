// File: models/public_address_policy.go

package models

import (
	"fmt"
	"strings"
)

const activityStreamsPublicAddress = "https://www.w3.org/ns/activitystreams#Public"

// PublicAddressDistributionPolicy controls whether the Public collection must
// appear in the primary `to` audience or may also appear only in `cc`.
type PublicAddressDistributionPolicy string

const (
	// PublicAddressExplicitPublicOnly relays only activities with Public in `to`.
	PublicAddressExplicitPublicOnly PublicAddressDistributionPolicy = "explicit_public_only"

	// PublicAddressPublicAndUnlisted preserves the pre-3.0 behavior by relaying
	// activities with Public in either `to` or `cc`.
	PublicAddressPublicAndUnlisted PublicAddressDistributionPolicy = "public_and_unlisted"
)

// ParsePublicAddressDistributionPolicy validates the operator-facing value.
// Empty input is the compatibility fallback for existing configurations.
func ParsePublicAddressDistributionPolicy(
	value string,
) (PublicAddressDistributionPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return PublicAddressPublicAndUnlisted, nil
	case string(PublicAddressExplicitPublicOnly):
		return PublicAddressExplicitPublicOnly, nil
	case string(PublicAddressPublicAndUnlisted):
		return PublicAddressPublicAndUnlisted, nil
	default:
		return "", fmt.Errorf(
			"unsupported value %q; supported values are %q and %q",
			value,
			PublicAddressExplicitPublicOnly,
			PublicAddressPublicAndUnlisted,
		)
	}
}

// String returns the validated operator-facing value.
func (policy PublicAddressDistributionPolicy) String() string {
	return string(policy)
}

// Allows reports whether the activity enters public-address fan-out.
func (policy PublicAddressDistributionPolicy) Allows(
	to []string,
	cc []string,
) bool {
	if containsPublicAddress(to) {
		return true
	}

	return policy == PublicAddressPublicAndUnlisted &&
		containsPublicAddress(cc)
}

// ExcludesPublicOnlyInCC identifies an activity that is public through `cc`
// but intentionally excluded from public fan-out by explicit_public_only.
func (policy PublicAddressDistributionPolicy) ExcludesPublicOnlyInCC(
	to []string,
	cc []string,
) bool {
	return policy == PublicAddressExplicitPublicOnly &&
		!containsPublicAddress(to) &&
		containsPublicAddress(cc)
}

func containsPublicAddress(addresses []string) bool {
	for _, address := range addresses {
		if address == activityStreamsPublicAddress {
			return true
		}
	}
	return false
}

// EOF: models/public_address_policy.go
