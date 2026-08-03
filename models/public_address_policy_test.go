// File: models/public_address_policy_test.go

package models

import "testing"

func TestParsePublicAddressDistributionPolicy(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  PublicAddressDistributionPolicy
	}{
		{
			name:  "omitted compatibility fallback",
			input: "",
			want:  PublicAddressPublicAndUnlisted,
		},
		{
			name:  "explicit public only",
			input: "explicit_public_only",
			want:  PublicAddressExplicitPublicOnly,
		},
		{
			name:  "public and unlisted",
			input: "public_and_unlisted",
			want:  PublicAddressPublicAndUnlisted,
		},
		{
			name:  "trimmed and case normalized",
			input: "  EXPLICIT_PUBLIC_ONLY  ",
			want:  PublicAddressExplicitPublicOnly,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParsePublicAddressDistributionPolicy(test.input)
			if err != nil {
				t.Fatalf("parse policy: %v", err)
			}
			if got != test.want {
				t.Fatalf("policy = %q; want %q", got, test.want)
			}
		})
	}
}

func TestParsePublicAddressDistributionPolicyRejectsUnknown(t *testing.T) {
	if _, err := ParsePublicAddressDistributionPolicy("everything"); err == nil {
		t.Fatal("unknown public-address policy was accepted")
	}
}

func TestPublicAddressDistributionPolicyAllows(t *testing.T) {
	public := activityStreamsPublicAddress
	followers := "https://publisher.example/actor/followers"

	tests := []struct {
		name   string
		policy PublicAddressDistributionPolicy
		to     []string
		cc     []string
		want   bool
	}{
		{
			name:   "explicit public in to",
			policy: PublicAddressExplicitPublicOnly,
			to:     []string{public},
			want:   true,
		},
		{
			name:   "explicit excludes public only in cc",
			policy: PublicAddressExplicitPublicOnly,
			to:     []string{followers},
			cc:     []string{public},
			want:   false,
		},
		{
			name:   "broad includes public only in cc",
			policy: PublicAddressPublicAndUnlisted,
			to:     []string{followers},
			cc:     []string{public},
			want:   true,
		},
		{
			name:   "followers only remains ineligible",
			policy: PublicAddressPublicAndUnlisted,
			to:     []string{followers},
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.policy.Allows(test.to, test.cc); got != test.want {
				t.Fatalf("Allows() = %t; want %t", got, test.want)
			}
		})
	}
}

func TestPublicAddressDistributionPolicyExcludesPublicOnlyInCC(t *testing.T) {
	public := activityStreamsPublicAddress
	followers := "https://publisher.example/actor/followers"

	if !PublicAddressExplicitPublicOnly.ExcludesPublicOnlyInCC(
		[]string{followers},
		[]string{public},
	) {
		t.Fatal("explicit policy did not identify cc-only Public")
	}

	if PublicAddressExplicitPublicOnly.ExcludesPublicOnlyInCC(
		[]string{public},
		nil,
	) {
		t.Fatal("explicit policy excluded Public in primary to")
	}

	if PublicAddressPublicAndUnlisted.ExcludesPublicOnlyInCC(
		[]string{followers},
		[]string{public},
	) {
		t.Fatal("broad policy excluded cc-only Public")
	}
}

// EOF: models/public_address_policy_test.go
