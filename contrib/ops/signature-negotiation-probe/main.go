package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	relayhttpsig "github.com/thystra/Activity-Relay/internal/httpsignature"
)

type report struct {
	Classification           string   `json:"classification"`
	FetchProfiles            []string `json:"fetch_profiles"`
	GenericFailureRequests   int      `json:"generic_failure_requests"`
	UnknownDeliveryProfile   string   `json:"unknown_delivery_profile"`
	ObservedDeliveryProfile  string   `json:"observed_delivery_profile"`
	RejectedDeliveryRequests int      `json:"rejected_delivery_requests"`
	FutureDeliveryProfile    string   `json:"future_delivery_profile"`
	DeliveryFallbackObserved bool     `json:"delivery_fallback_observed"`
}

func profileFromRequest(request *http.Request) relayhttpsig.Profile {
	if request.Header.Get("Signature-Input") != "" {
		return relayhttpsig.ProfileRFC9421
	}
	return relayhttpsig.ProfileLegacy
}

func main() {
	var redisURL string
	var evidenceDir string
	flag.StringVar(
		&redisURL,
		"redis-url",
		"",
		"Redis URL",
	)
	flag.StringVar(
		&evidenceDir,
		"evidence-dir",
		"",
		"private evidence directory",
	)
	flag.Parse()

	if redisURL == "" || evidenceDir == "" {
		fmt.Fprintln(
			os.Stderr,
			"--redis-url and --evidence-dir are required",
		)
		os.Exit(2)
	}
	if err := os.MkdirAll(evidenceDir, 0700); err != nil {
		panic(err)
	}

	options, err := redis.ParseURL(redisURL)
	if err != nil {
		panic(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		panic(err)
	}

	store, err := relayhttpsig.NewRedisDestinationCapabilityStore(
		client,
		fmt.Sprintf(
			"probe:http-signature:%d:",
			time.Now().UnixNano(),
		),
	)
	if err != nil {
		panic(err)
	}
	negotiator, err := relayhttpsig.NewDestinationNegotiator(
		relayhttpsig.DestinationNegotiatorOptions{
			Store: store,
		},
	)
	if err != nil {
		panic(err)
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	keyID := "https://relay.example/actor#main-key"
	signer, err := relayhttpsig.NewNegotiatingSigner(
		keyID,
		privateKey,
		relayhttpsig.ProfileDual,
		negotiator,
	)
	if err != nil {
		panic(err)
	}

	result := report{
		Classification: "signature_negotiation_runtime_fail",
	}

	var fetchMutex sync.Mutex
	fetchServer := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			profile := profileFromRequest(request)
			fetchMutex.Lock()
			result.FetchProfiles = append(
				result.FetchProfiles,
				profile.String(),
			)
			fetchMutex.Unlock()
			if profile == relayhttpsig.ProfileRFC9421 {
				writer.Header().Set(
					"WWW-Authenticate",
					`Signature realm="activitypub"`,
				)
				http.Error(
					writer,
					"legacy required",
					http.StatusUnauthorized,
				)
				return
			}
			writer.Header().Set(
				"Content-Type",
				"application/activity+json",
			)
			io.WriteString(
				writer,
				`{"id":"https://remote.example/actor"}`,
			)
		},
	))
	defer fetchServer.Close()

	getRequest, err := http.NewRequest(
		http.MethodGet,
		fetchServer.URL+"/actor",
		nil,
	)
	if err != nil {
		panic(err)
	}
	fetchResponse, err := signer.DoGET(
		fetchServer.Client(),
		getRequest,
	)
	if err != nil {
		panic(err)
	}
	io.Copy(io.Discard, fetchResponse.Body)
	fetchResponse.Body.Close()
	if fetchResponse.StatusCode != http.StatusOK {
		panic(
			fmt.Sprintf(
				"fallback fetch status %d",
				fetchResponse.StatusCode,
			),
		)
	}

	genericServer := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			result.GenericFailureRequests++
			http.Error(
				writer,
				"generic unauthorized",
				http.StatusUnauthorized,
			)
		},
	))
	defer genericServer.Close()
	genericRequest, err := http.NewRequest(
		http.MethodGet,
		genericServer.URL+"/actor",
		nil,
	)
	if err != nil {
		panic(err)
	}
	genericResponse, err := signer.DoGET(
		genericServer.Client(),
		genericRequest,
	)
	if err != nil {
		panic(err)
	}
	genericResponse.Body.Close()

	deliveryServer := httptest.NewServer(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			result.RejectedDeliveryRequests++
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
	defer deliveryServer.Close()
	deliveryURL := deliveryServer.URL + "/inbox"

	unknownPlan, err := signer.Plan(
		ctx,
		relayhttpsig.DestinationScopeDelivery,
		deliveryURL,
	)
	if err != nil {
		panic(err)
	}
	result.UnknownDeliveryProfile =
		unknownPlan.Primary.String()
	if unknownPlan.HasFallback() {
		result.DeliveryFallbackObserved = true
	}

	acceptHeader := make(http.Header)
	acceptHeader.Set(
		"Accept-Signature",
		`activitypub=("@method" "@authority" "@target-uri" "content-digest" "content-type" "date");created;keyid="https://relay.example/actor#main-key";alg="rsa-v1_5-sha256";tag="activitypub"`,
	)
	if err := signer.ObserveResponse(
		ctx,
		relayhttpsig.DestinationScopeDelivery,
		deliveryURL,
		relayhttpsig.ProfileLegacy,
		http.StatusAccepted,
		acceptHeader,
	); err != nil {
		panic(err)
	}
	modernPlan, err := signer.Plan(
		ctx,
		relayhttpsig.DestinationScopeDelivery,
		deliveryURL,
	)
	if err != nil {
		panic(err)
	}
	result.ObservedDeliveryProfile =
		modernPlan.Primary.String()

	// Redis capability observations use millisecond ordering and reject equal
	// timestamps. Keep this synthetic local probe deterministic.
	time.Sleep(2 * time.Millisecond)

	body := []byte(`{"type":"Announce"}`)
	postRequest, err := http.NewRequest(
		http.MethodPost,
		deliveryURL,
		bytes.NewReader(body),
	)
	if err != nil {
		panic(err)
	}
	postRequest.Header.Set(
		"Content-Type",
		"application/activity+json",
	)
	if err := signer.SignPOSTWithProfile(
		postRequest,
		body,
		modernPlan.Primary,
	); err != nil {
		panic(err)
	}
	postResponse, err := deliveryServer.Client().Do(postRequest)
	if err != nil {
		panic(err)
	}
	io.Copy(io.Discard, postResponse.Body)
	postResponse.Body.Close()
	if err := signer.ObserveResponse(
		ctx,
		relayhttpsig.DestinationScopeDelivery,
		deliveryURL,
		modernPlan.Primary,
		postResponse.StatusCode,
		postResponse.Header,
	); err != nil {
		panic(err)
	}
	futurePlan, err := signer.Plan(
		ctx,
		relayhttpsig.DestinationScopeDelivery,
		deliveryURL,
	)
	if err != nil {
		panic(err)
	}
	result.FutureDeliveryProfile =
		futurePlan.Primary.String()
	if futurePlan.HasFallback() {
		result.DeliveryFallbackObserved = true
	}

	expectedFetch := []string{"rfc9421", "legacy"}
	if fmt.Sprint(result.FetchProfiles) != fmt.Sprint(expectedFetch) {
		panic(
			fmt.Sprintf(
				"fetch profiles = %v",
				result.FetchProfiles,
			),
		)
	}
	if result.GenericFailureRequests != 1 {
		panic(
			fmt.Sprintf(
				"generic failure requests = %d",
				result.GenericFailureRequests,
			),
		)
	}
	if result.UnknownDeliveryProfile != "legacy" {
		panic(
			errors.New(
				"unknown delivery did not select legacy",
			),
		)
	}
	if result.ObservedDeliveryProfile != "rfc9421" {
		panic(
			errors.New(
				"Accept-Signature did not select RFC 9421",
			),
		)
	}
	if result.RejectedDeliveryRequests != 1 {
		panic(
			fmt.Sprintf(
				"delivery request count = %d",
				result.RejectedDeliveryRequests,
			),
		)
	}
	if result.FutureDeliveryProfile != "legacy" {
		panic(
			errors.New(
				"explicit rejection did not select future legacy",
			),
		)
	}
	if result.DeliveryFallbackObserved {
		panic(errors.New("delivery fallback was observed"))
	}

	result.Classification =
		"signature_negotiation_runtime_pass"
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		panic(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(
		filepath.Join(evidenceDir, "report.json"),
		encoded,
		0600,
	); err != nil {
		panic(err)
	}
	fmt.Println(result.Classification)
}
