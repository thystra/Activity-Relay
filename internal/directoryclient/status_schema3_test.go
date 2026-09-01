package directoryclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestStatusAcceptsSchema3PublicListingFields(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != statusPath {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(
			response,
			`{"schema_version":3,"service":"activity-relay-directory","version":"0.1.0-rc4","public_base_url":%q,"lifecycle_enabled":true,"lifecycle_available":true,"enrollment_open":true,"public_listing_enabled":true,"public_listing_available":true}`,
			server.URL,
		)
	}))
	defer server.Close()

	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	client := &Client{
		origin:     origin,
		httpClient: server.Client(),
	}

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.SchemaVersion != 3 ||
		status.Version != "0.1.0-rc4" ||
		!status.LifecycleEnabled ||
		!status.LifecycleAvailable ||
		!status.EnrollmentOpen ||
		!status.PublicListingEnabled ||
		!status.PublicListingAvailable {
		t.Fatalf("Status() = %#v", status)
	}
}
