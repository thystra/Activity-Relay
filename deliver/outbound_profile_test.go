package deliver

import "testing"

func TestWorkerConfiguredOutboundSignerMatchesRelayConfig(t *testing.T) {
	if OutboundRequestSigner == nil {
		t.Fatal("worker outbound request signer is nil")
	}
	if got, want := OutboundRequestSigner.Profile(),
		GlobalConfig.OutboundSignatureProfile(); got != want {
		t.Fatalf("worker outbound profile = %q; want %q", got, want)
	}
}
