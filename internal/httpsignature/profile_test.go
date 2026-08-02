package httpsignature

import (
	"errors"
	"testing"
)

func TestParseProfile(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      Profile
		wantError bool
	}{
		{name: "empty defaults to legacy", input: "", want: ProfileLegacy},
		{name: "whitespace defaults to legacy", input: "  ", want: ProfileLegacy},
		{name: "legacy", input: "legacy", want: ProfileLegacy},
		{name: "case normalized", input: " RFC9421 ", want: ProfileRFC9421},
		{name: "dual", input: "dual", want: ProfileDual},
		{name: "unknown", input: "modern", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseProfile(test.input)
			if test.wantError {
				if err == nil {
					t.Fatal("expected profile parsing error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parse profile: %v", err)
			}
			if got != test.want {
				t.Fatalf("profile = %q; want %q", got, test.want)
			}
		})
	}
}

func TestDualProfileRequiresDeliveryPolicy(t *testing.T) {
	signer, _ := newTestSigner(t)

	request := newSignedProfileTestRequest(t, "GET", nil)
	err := signer.SignGETWithProfile(request, ProfileDual)
	if !errors.Is(err, ErrDualProfileRequiresDeliveryPolicy) {
		t.Fatalf("dual profile error = %v", err)
	}
	if request.Header.Get("Signature") != "" {
		t.Fatal("dual profile primitive unexpectedly signed the request")
	}
}
