package httpsignature

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newDualSignerForTest(
	t *testing.T,
) (*ConfiguredSigner, *memoryDestinationCapabilityStore) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryDestinationCapabilityStore{save: true}
	negotiator := testNegotiator(
		t,
		store,
		time.Now().UTC(),
	)
	signer, err := NewNegotiatingSigner(
		"https://relay.example/actor#main-key",
		privateKey,
		ProfileDual,
		negotiator,
	)
	if err != nil {
		t.Fatal(err)
	}
	return signer, store
}

func TestDualGETFallsBackOnceAfterExplicitChallenge(t *testing.T) {
	signer, _ := newDualSignerForTest(t)
	var mutex sync.Mutex
	var profiles []Profile

	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			profile := ProfileLegacy
			if request.Header.Get("Signature-Input") != "" {
				profile = ProfileRFC9421
			}
			mutex.Lock()
			profiles = append(profiles, profile)
			mutex.Unlock()

			if profile == ProfileRFC9421 {
				writer.Header().Set(
					"WWW-Authenticate",
					`Signature realm="activitypub"`,
				)
				http.Error(
					writer,
					"legacy signature required",
					http.StatusUnauthorized,
				)
				return
			}
			writer.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	request, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/actor",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := signer.DoGET(server.Client(), request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fallback status = %d", response.StatusCode)
	}
	if err := request.Context().Err(); err != nil {
		t.Fatalf("caller request context was canceled: %v", err)
	}

	mutex.Lock()
	defer mutex.Unlock()
	if fmt.Sprint(profiles) !=
		fmt.Sprint([]Profile{ProfileRFC9421, ProfileLegacy}) {
		t.Fatalf("GET profiles = %v", profiles)
	}
}

func TestDualGETDoesNotFallbackOnGeneric401(t *testing.T) {
	signer, _ := newDualSignerForTest(t)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			requestCount++
			http.Error(
				writer,
				"signature rejected",
				http.StatusUnauthorized,
			)
		},
	))
	defer server.Close()

	request, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/actor",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := signer.DoGET(server.Client(), request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if requestCount != 1 {
		t.Fatalf("generic 401 request count = %d; want 1", requestCount)
	}
}

func TestObserveAcceptSignatureChangesFutureDeliveryPlan(
	t *testing.T,
) {
	signer, _ := newDualSignerForTest(t)
	destination := "https://remote.example/inbox"
	before, err := signer.Plan(
		context.Background(),
		DestinationScopeDelivery,
		destination,
	)
	if err != nil {
		t.Fatal(err)
	}
	if before.Primary != ProfileLegacy {
		t.Fatalf("unknown delivery profile = %q", before.Primary)
	}

	header := make(http.Header)
	header.Set(
		"Accept-Signature",
		`activitypub=("@method" "@authority" "@target-uri" "content-digest" "content-type" "date");created;keyid="https://relay.example/actor#main-key";alg="rsa-v1_5-sha256";tag="activitypub"`,
	)
	if err := signer.ObserveResponse(
		context.Background(),
		DestinationScopeDelivery,
		destination,
		ProfileLegacy,
		http.StatusAccepted,
		header,
	); err != nil {
		t.Fatal(err)
	}
	after, err := signer.Plan(
		context.Background(),
		DestinationScopeDelivery,
		destination,
	)
	if err != nil {
		t.Fatal(err)
	}
	if after.Primary != ProfileRFC9421 {
		t.Fatalf("observed delivery profile = %q", after.Primary)
	}
}

func TestRFC9421DeliveryRejectionChangesOnlyFuturePlan(
	t *testing.T,
) {
	signer, _ := newDualSignerForTest(t)
	destination := "https://remote.example/inbox"
	header := make(http.Header)
	header.Set(
		"WWW-Authenticate",
		`Signature realm="activitypub"`,
	)
	if err := signer.ObserveResponse(
		context.Background(),
		DestinationScopeDelivery,
		destination,
		ProfileRFC9421,
		http.StatusUnauthorized,
		header,
	); err != nil {
		t.Fatal(err)
	}
	plan, err := signer.Plan(
		context.Background(),
		DestinationScopeDelivery,
		destination,
	)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Primary != ProfileLegacy || plan.HasFallback() {
		t.Fatalf("future delivery plan = %+v", plan)
	}
}
