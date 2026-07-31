// File: deliver/sender_response_test.go
package deliver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendActivityWithResponseCapturesBoundedFailureDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusFailedDependency)
		_, _ = w.Write([]byte(strings.Repeat("dependency failure ", 400)))
	}))
	defer server.Close()

	response, err := sendActivityWithResponse(
		server.URL,
		RelayActor.PublicKey.ID,
		[]byte(`{"type":"Announce"}`),
		GlobalConfig.ActorKey(),
	)
	if err == nil {
		t.Fatal("non-success delivery returned nil error")
	}
	if response.StatusCode != http.StatusFailedDependency {
		t.Fatalf(
			"status code = %d; want %d",
			response.StatusCode,
			http.StatusFailedDependency,
		)
	}
	if response.Body == "" {
		t.Fatal("bounded response body is empty")
	}
	if !response.BodyTruncated {
		t.Fatal("expected oversized response body to be marked truncated")
	}
	if len(response.Body) > int(maxDeliveryResponseBodyBytes)+len(" [truncated]") {
		t.Fatalf("bounded response body length = %d", len(response.Body))
	}
}

// EOF: deliver/sender_response_test.go
