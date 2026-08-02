package httpsignature

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestParseOutboundProfile(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      Profile
		wantError error
	}{
		{
			name: "empty defaults to legacy",
			want: ProfileLegacy,
		},
		{
			name:  "legacy",
			input: "legacy",
			want:  ProfileLegacy,
		},
		{
			name:  "RFC 9421 normalized",
			input: " RFC9421 ",
			want:  ProfileRFC9421,
		},
		{
			name:  "dual",
			input: "dual",
			want:  ProfileDual,
		},
		{
			name:      "unknown rejected",
			input:     "automatic",
			wantError: errors.New("unsupported"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseOutboundProfile(test.input)
			if test.wantError != nil {
				if err == nil {
					t.Fatal("expected outbound profile error")
				}
				if errors.Is(
					test.wantError,
					ErrDualProfileRequiresDeliveryPolicy,
				) && !errors.Is(
					err,
					ErrDualProfileRequiresDeliveryPolicy,
				) {
					t.Fatalf("outbound profile error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse outbound profile: %v", err)
			}
			if got != test.want {
				t.Fatalf("profile = %q; want %q", got, test.want)
			}
		})
	}
}

func TestConfiguredSignerPreservesLegacyDefault(t *testing.T) {
	baseSigner, publicKey := newTestSigner(t)
	signer, err := NewConfiguredSigner(
		baseSigner.keyID,
		baseSigner.privateKey,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}
	if signer.Profile() != ProfileLegacy {
		t.Fatalf("profile = %q; want legacy", signer.Profile())
	}

	body := []byte(`{"type":"Follow"}`)
	request := newSignedProfileTestRequest(t, http.MethodPost, body)
	if err := signer.SignPOST(request, body); err != nil {
		t.Fatal(err)
	}
	if request.Header.Get("Digest") == "" {
		t.Fatal("legacy configured signer has no Digest field")
	}
	if request.Header.Get("Signature-Input") != "" {
		t.Fatal("legacy configured signer emitted Signature-Input")
	}
	if err := verifyRequestError(request, publicKey); err != nil {
		t.Fatalf("verify legacy configured signer: %v", err)
	}
}

func TestConfiguredSignerUsesRFC9421ForGETAndPOST(t *testing.T) {
	baseSigner, publicKey := newTestSigner(t)
	signer, err := NewConfiguredSigner(
		baseSigner.keyID,
		baseSigner.privateKey,
		ProfileRFC9421,
	)
	if err != nil {
		t.Fatal(err)
	}

	getRequest := newSignedProfileTestRequest(t, http.MethodGet, nil)
	if err := signer.SignGET(getRequest); err != nil {
		t.Fatal(err)
	}
	if getRequest.Header.Get("Signature-Input") == "" {
		t.Fatal("RFC 9421 configured GET has no Signature-Input")
	}
	if err := verifyRFC9421Request(getRequest, publicKey); err != nil {
		t.Fatalf("verify configured RFC 9421 GET: %v", err)
	}

	body := []byte(`{"type":"Follow"}`)
	postRequest := newSignedProfileTestRequest(t, http.MethodPost, body)
	if err := signer.SignPOST(postRequest, body); err != nil {
		t.Fatal(err)
	}
	if postRequest.Header.Get("Content-Digest") == "" {
		t.Fatal("RFC 9421 configured POST has no Content-Digest")
	}
	if postRequest.Header.Get("Digest") != "" {
		t.Fatal("RFC 9421 configured POST retained legacy Digest")
	}
	if err := verifyRFC9421Request(postRequest, publicKey); err != nil {
		t.Fatalf("verify configured RFC 9421 POST: %v", err)
	}
}

func TestConfiguredSignerRejectsDual(t *testing.T) {
	baseSigner, _ := newTestSigner(t)
	_, err := NewConfiguredSigner(
		baseSigner.keyID,
		baseSigner.privateKey,
		ProfileDual,
	)
	if !errors.Is(err, ErrDualProfileRequiresDeliveryPolicy) {
		t.Fatalf("dual configured signer error = %v", err)
	}
}

func TestNegotiatingSignerAcceptsDual(t *testing.T) {
	baseSigner, _ := newTestSigner(t)
	negotiator := testNegotiator(
		t,
		&memoryDestinationCapabilityStore{save: true},
		time.Now().UTC(),
	)
	signer, err := NewNegotiatingSigner(
		baseSigner.keyID,
		baseSigner.privateKey,
		ProfileDual,
		negotiator,
	)
	if err != nil {
		t.Fatal(err)
	}
	if signer.Profile() != ProfileDual {
		t.Fatalf("profile = %q; want dual", signer.Profile())
	}
}
