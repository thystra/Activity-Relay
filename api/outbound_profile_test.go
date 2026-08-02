package api

import "testing"

func TestAPIConfiguredOutboundSignerMatchesRelayConfig(t *testing.T) {
	if RemoteRequestSigner == nil {
		t.Fatal("API outbound request signer is nil")
	}
	if got, want := RemoteRequestSigner.Profile(),
		GlobalConfig.OutboundSignatureProfile(); got != want {
		t.Fatalf("API outbound profile = %q; want %q", got, want)
	}
}
