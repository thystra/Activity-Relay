package httpsignature

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
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

func TestDualGETFallsBackAfterAmbiguous400AndCachesLegacy(t *testing.T) {
	signer, store := newDualSignerForTest(t)
	var profiles []Profile
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			profile := ProfileLegacy
			if request.Header.Get("Signature-Input") != "" {
				profile = ProfileRFC9421
			}
			profiles = append(profiles, profile)
			if profile == ProfileRFC9421 {
				http.Error(writer, "Bad Request", http.StatusBadRequest)
				return
			}
			writer.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/actor", nil)
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
	if fmt.Sprint(profiles) !=
		fmt.Sprint([]Profile{ProfileRFC9421, ProfileLegacy}) {
		t.Fatalf("GET profiles = %v", profiles)
	}
	if !store.found ||
		store.capability.Profile != ProfileLegacy ||
		store.capability.Scope != DestinationScopeFetch ||
		store.capability.Evidence != CapabilityEvidenceSuccessfulLegacyFallback {
		t.Fatalf("cached fallback capability = %+v", store.capability)
	}

	profiles = nil
	request, err = http.NewRequest(http.MethodGet, server.URL+"/actor", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = signer.DoGET(server.Client(), request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if fmt.Sprint(profiles) != fmt.Sprint([]Profile{ProfileLegacy}) {
		t.Fatalf("cached GET profiles = %v", profiles)
	}
}

func TestDualGETFailedAmbiguousFallbackDoesNotCacheLegacy(t *testing.T) {
	for _, fallbackStatus := range []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(fallbackStatus), func(t *testing.T) {
			signer, store := newDualSignerForTest(t)
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, request *http.Request) {
					requestCount++
					status := http.StatusBadRequest
					if request.Header.Get("Signature-Input") == "" {
						status = fallbackStatus
					}
					http.Error(writer, http.StatusText(status), status)
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
			if response.StatusCode != fallbackStatus {
				t.Fatalf("fallback status = %d", response.StatusCode)
			}
			if requestCount != 2 {
				t.Fatalf("request count = %d; want 2", requestCount)
			}
			if store.found {
				t.Fatalf(
					"failed fallback cached capability %+v",
					store.capability,
				)
			}
		})
	}
}

func TestDualGETDoesNotFallbackOnOtherFailures(t *testing.T) {
	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusNotFound,
		http.StatusGone,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			signer, _ := newDualSignerForTest(t)
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(
				func(writer http.ResponseWriter, request *http.Request) {
					requestCount++
					http.Error(writer, http.StatusText(status), status)
				},
			))
			defer server.Close()

			request, err := http.NewRequest(http.MethodGet, server.URL+"/actor", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := signer.DoGET(server.Client(), request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if requestCount != 1 {
				t.Fatalf("request count = %d; want 1", requestCount)
			}
		})
	}
}

type failingRoundTripper struct {
	requestCount int
}

func (transport *failingRoundTripper) RoundTrip(
	*http.Request,
) (*http.Response, error) {
	transport.requestCount++
	return nil, errors.New("transport failed")
}

func TestDualGETTransportFailureDoesNotFallback(t *testing.T) {
	signer, store := newDualSignerForTest(t)
	transport := &failingRoundTripper{}
	client := &http.Client{Transport: transport}
	request, err := http.NewRequest(
		http.MethodGet,
		"https://remote.example/actor",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.DoGET(client, request); err == nil {
		t.Fatal("transport failure was not returned")
	}
	if transport.requestCount != 1 {
		t.Fatalf("request count = %d; want 1", transport.requestCount)
	}
	if store.found {
		t.Fatalf("transport failure cached capability %+v", store.capability)
	}
}

func TestCachedRFC9421GETDoesNotFallbackOn400(t *testing.T) {
	signer, store := newDualSignerForTest(t)
	now := time.Now().UTC()
	store.capability = DestinationCapability{
		Origin:     "http://placeholder.invalid",
		Scope:      DestinationScopeFetch,
		Profile:    ProfileRFC9421,
		Evidence:   CapabilityEvidenceSuccessfulRFC9421,
		ObservedAt: now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
	}
	store.found = true

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			requestCount++
			http.Error(writer, "Bad Request", http.StatusBadRequest)
		},
	))
	defer server.Close()
	store.capability.Origin = server.URL

	request, err := http.NewRequest(http.MethodGet, server.URL+"/actor", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := signer.DoGET(server.Client(), request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if requestCount != 1 {
		t.Fatalf("cached RFC 9421 request count = %d; want 1", requestCount)
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
