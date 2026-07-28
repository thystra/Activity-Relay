package deliver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-fed/httpsig"
)

type signatureObservation struct {
	host string
	err  error
}

func TestSendActivitySignsWireHostIncludingPort(t *testing.T) {
	body := []byte(`{"@context":"https://www.w3.org/ns/activitystreams","type":"Accept"}`)
	observations := make(chan signatureObservation, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifier, err := httpsig.NewVerifier(r)
		if err == nil {
			err = verifier.Verify(
				GlobalConfig.ActorKey().Public(),
				httpsig.RSA_SHA256,
			)
		}
		observations <- signatureObservation{
			host: r.Host,
			err:  err,
		}
		if err != nil {
			http.Error(
				w,
				"HTTP signature verification failed",
				http.StatusBadRequest,
			)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	previousClient := HttpClient
	HttpClient = server.Client()
	defer func() {
		HttpClient = previousClient
	}()

	err := sendActivity(
		server.URL+"/inbox",
		RelayActor.PublicKey.ID,
		body,
		GlobalConfig.ActorKey(),
	)
	if err != nil {
		t.Fatalf("Expected signed delivery to succeed, got: %v", err)
	}

	var observation signatureObservation
	select {
	case observation = <-observations:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for the receiving server observation")
	}

	expectedHost := strings.TrimPrefix(server.URL, "http://")
	if observation.host != expectedHost {
		t.Fatalf(
			"Expected wire Host %q, got %q",
			expectedHost,
			observation.host,
		)
	}
	if observation.err != nil {
		t.Fatalf(
			"Receiver-style HTTP signature verification failed: %v",
			observation.err,
		)
	}
}

func TestSendActivityIncludesBoundedErrorResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(
			"HTTP signature verification failed.\n",
		))
	}))
	defer server.Close()

	previousClient := HttpClient
	HttpClient = server.Client()
	defer func() {
		HttpClient = previousClient
	}()

	err := sendActivity(
		server.URL+"/inbox",
		RelayActor.PublicKey.ID,
		[]byte(`{"type":"Accept"}`),
		GlobalConfig.ActorKey(),
	)
	if err == nil {
		t.Fatal("Expected a non-2xx delivery response to return an error")
	}

	message := err.Error()
	if !strings.Contains(message, "400 Bad Request") {
		t.Fatalf("Expected status in delivery error, got: %s", message)
	}
	if !strings.Contains(
		message,
		"HTTP signature verification failed.",
	) {
		t.Fatalf(
			"Expected bounded response body in delivery error, got: %s",
			message,
		)
	}
}
