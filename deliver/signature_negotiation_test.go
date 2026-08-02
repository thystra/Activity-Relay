package deliver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

func TestDeliveryProfileFromTaskArgs(t *testing.T) {
	profile, err := deliveryProfileFromArgs(
		[]string{"inbox", "activity", "rfc9421"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if profile != relayhttpsig.ProfileRFC9421 {
		t.Fatalf("task profile = %q", profile)
	}

	if _, err := deliveryProfileFromArgs(
		[]string{"inbox", "activity", "dual"},
	); err == nil {
		t.Fatal("dual wire profile was accepted")
	}
}

func TestLegacyQueuedTaskCompatibility(t *testing.T) {
	profile, err := deliveryProfileFromArgs(
		[]string{"inbox", "activity"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if profile == relayhttpsig.ProfileDual || profile == "" {
		t.Fatalf("legacy queued task profile = %q", profile)
	}
}

func TestExplicitDeliveryProfileSendsOnlyOnce(t *testing.T) {
	previousClient := HttpClient
	t.Cleanup(func() {
		HttpClient = previousClient
	})

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			requestCount++
			if request.Header.Get("Signature-Input") == "" {
				http.Error(
					writer,
					"missing RFC 9421 signature",
					http.StatusUnauthorized,
				)
				return
			}
			writer.Header().Set(
				"WWW-Authenticate",
				`Signature realm="activitypub"`,
			)
			http.Error(
				writer,
				"legacy required",
				http.StatusUnauthorized,
			)
		},
	))
	defer server.Close()
	HttpClient = server.Client()

	body := []byte(`{"type":"Announce"}`)
	response, err := sendActivityWithResponseProfile(
		server.URL+"/inbox",
		RelayActor.PublicKey.ID,
		body,
		GlobalConfig.ActorKey(),
		relayhttpsig.ProfileRFC9421,
	)
	if err == nil {
		t.Fatal("expected delivery rejection")
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("delivery status = %d", response.StatusCode)
	}
	if requestCount != 1 {
		t.Fatalf("delivery request count = %d; want 1", requestCount)
	}

	if err := OutboundRequestSigner.ObserveResponse(
		context.Background(),
		relayhttpsig.DestinationScopeDelivery,
		server.URL+"/inbox",
		relayhttpsig.ProfileRFC9421,
		response.StatusCode,
		response.Header,
	); err != nil {
		t.Fatal(err)
	}
}
