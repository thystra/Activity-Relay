package httpsignature

import (
	"net/http"
	"testing"
)

const acceptSignatureTestKeyID = "https://relay.example/actor#main-key"

func TestCompatibleAcceptSignature(t *testing.T) {
	tests := []struct {
		name   string
		scope  DestinationScope
		value  string
		accept bool
	}{
		{
			name:   "fetch exact",
			scope:  DestinationScopeFetch,
			value:  `activitypub=("@method" "@authority" "@target-uri" "date");created;keyid="https://relay.example/actor#main-key";alg="rsa-v1_5-sha256";tag="activitypub"`,
			accept: true,
		},
		{
			name:   "delivery exact",
			scope:  DestinationScopeDelivery,
			value:  `activitypub=("@method" "@authority" "@target-uri" "content-digest" "content-type" "date");created`,
			accept: true,
		},
		{
			name:  "additional member",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date"), other=("@method")`,
		},
		{
			name:  "wrong order",
			scope: DestinationScopeFetch,
			value: `activitypub=("@authority" "@method" "@target-uri" "date")`,
		},
		{
			name:  "component parameter",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method";sf "@authority" "@target-uri" "date")`,
		},
		{
			name:  "expires unsupported",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date");expires`,
		},
		{
			name:  "nonce unsupported",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date");nonce="fixed"`,
		},
		{
			name:  "wrong algorithm",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date");alg="rsa-pss-sha512"`,
		},
		{
			name:  "wrong key",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date");keyid="https://other.example/key"`,
		},
		{
			name:  "wrong tag",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date");tag="other"`,
		},
		{
			name:  "unknown parameter",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method" "@authority" "@target-uri" "date");future`,
		},
		{
			name:  "malformed",
			scope: DestinationScopeFetch,
			value: `activitypub=("@method"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CompatibleAcceptSignature(
				[]string{test.value},
				test.scope,
				acceptSignatureTestKeyID,
			)
			if got != test.accept {
				t.Fatalf(
					"CompatibleAcceptSignature() = %v; want %v",
					got,
					test.accept,
				)
			}
		})
	}
}

func TestExplicitLegacySignatureRejection(t *testing.T) {
	tests := []struct {
		name   string
		status int
		header string
		want   bool
	}{
		{
			name:   "401 signature",
			status: http.StatusUnauthorized,
			header: `Signature realm="activitypub",headers="(request-target) host date"`,
			want:   true,
		},
		{
			name:   "403 combined challenge",
			status: http.StatusForbidden,
			header: `Basic realm="site", Signature realm="activitypub"`,
			want:   true,
		},
		{
			name:   "401 basic only",
			status: http.StatusUnauthorized,
			header: `Basic realm="Signature is required"`,
		},
		{
			name:   "generic 400",
			status: http.StatusBadRequest,
			header: `Signature realm="activitypub"`,
		},
		{
			name:   "body text is not header evidence",
			status: http.StatusUnauthorized,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := make(http.Header)
			if test.header != "" {
				header.Set("WWW-Authenticate", test.header)
			}
			got := ExplicitLegacySignatureRejection(
				test.status,
				header,
			)
			if got != test.want {
				t.Fatalf(
					"ExplicitLegacySignatureRejection() = %v; want %v",
					got,
					test.want,
				)
			}
		})
	}
}
