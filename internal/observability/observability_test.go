package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrivateRegistryAllowsIndependentServices(t *testing.T) {
	ready := func(context.Context) error { return nil }
	first := newService("test-one", ready)
	second := newService("test-two", ready)

	for name, service := range map[string]*Service{"first": first, "second": second} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		service.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s metrics status = %d; want 200", name, recorder.Code)
		}
		body := recorder.Body.String()
		if !strings.Contains(body, "activity_relay_build_info") {
			t.Fatalf("%s metrics omitted build info", name)
		}
		if !strings.Contains(body, "go_goroutines") {
			t.Fatalf("%s metrics omitted Go runtime metrics", name)
		}
		if !strings.Contains(body, "process_start_time_seconds") {
			t.Fatalf("%s metrics omitted process metrics", name)
		}
	}
}

func TestProbeEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		method     string
		readiness  ReadinessFunc
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{
			name:       "healthy",
			path:       "/-/healthy",
			method:     http.MethodGet,
			readiness:  func(context.Context) error { return errors.New("unused") },
			wantStatus: http.StatusOK,
			wantBody:   "healthy\n",
		},
		{
			name:       "ready",
			path:       "/-/ready",
			method:     http.MethodGet,
			readiness:  func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   "ready\n",
		},
		{
			name:       "not ready",
			path:       "/-/ready",
			method:     http.MethodGet,
			readiness:  func(context.Context) error { return errors.New("Redis unavailable") },
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "not ready\n",
		},
		{
			name:       "head",
			path:       "/-/healthy",
			method:     http.MethodHead,
			readiness:  func(context.Context) error { return nil },
			wantStatus: http.StatusOK,
			wantBody:   "",
		},
		{
			name:       "method not allowed",
			path:       "/-/ready",
			method:     http.MethodPost,
			readiness:  func(context.Context) error { return nil },
			wantStatus: http.StatusMethodNotAllowed,
			wantBody:   "method not allowed\n",
			wantAllow:  "GET, HEAD",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newService("test", test.readiness)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, nil)
			service.Handler().ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d; want %d", recorder.Code, test.wantStatus)
			}
			if recorder.Body.String() != test.wantBody {
				t.Fatalf("body = %q; want %q", recorder.Body.String(), test.wantBody)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q; want no-store", got)
			}
			if got := recorder.Header().Get("Allow"); got != test.wantAllow {
				t.Errorf("Allow = %q; want %q", got, test.wantAllow)
			}
		})
	}
}

func TestReadinessTimeout(t *testing.T) {
	service := newService("test", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	started := time.Now()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/-/ready", nil)
	service.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503", recorder.Code)
	}
	elapsed := time.Since(started)
	if elapsed < readinessTimeout-50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("readiness duration = %s; expected bounded timeout near %s", elapsed, readinessTimeout)
	}
}

func TestInstrumentUsesBoundedLabels(t *testing.T) {
	service := newService("test", func(context.Context) error { return nil })
	handler := service.Instrument(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/inbox?actor=ignored", nil),
		httptest.NewRequest("CUSTOM", "/private/12345", nil),
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
	}

	metrics := httptest.NewRecorder()
	service.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := metrics.Body.String()

	for _, expected := range []string{
		`activity_relay_http_requests_total{code="202",method="POST",route="/inbox"} 1`,
		`activity_relay_http_requests_total{code="202",method="OTHER",route="unknown"} 1`,
		`activity_relay_http_request_duration_seconds_count{code="202",method="POST",route="/inbox"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("metrics omitted %q\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"private/12345", "actor=ignored", "CUSTOM"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("metrics unexpectedly contain unbounded value %q", forbidden)
		}
	}
}

func TestInstrumentRecordsPanicsAndRepanics(t *testing.T) {
	service := newService("test", func(context.Context) error { return nil })
	handler := service.Instrument(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected instrumented handler to repanic")
			}
		}()
		handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/actor", nil),
		)
	}()

	metrics := httptest.NewRecorder()
	service.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(
		metrics.Body.String(),
		`activity_relay_http_requests_total{code="500",method="GET",route="/actor"} 1`,
	) {
		t.Fatal("panic was not recorded as HTTP 500")
	}
}

func TestNormalizedRouteAndMethod(t *testing.T) {
	for _, path := range []string{
		"/.well-known/nodeinfo",
		"/.well-known/webfinger",
		"/nodeinfo/2.1",
		"/status.json",
		"/actor",
		"/actor/outbox",
		"/actor/followers",
		"/actor/following",
		"/inbox",
	} {
		if got := normalizedRoute(path); got != path {
			t.Errorf("normalizedRoute(%q) = %q", path, got)
		}
	}
	if got := normalizedRoute("/actor/unknown"); got != "unknown" {
		t.Errorf("unknown route = %q", got)
	}
	if got := normalizedMethod(http.MethodPatch); got != http.MethodPatch {
		t.Errorf("PATCH method = %q", got)
	}
	if got := normalizedMethod("BREW"); got != "OTHER" {
		t.Errorf("custom method = %q", got)
	}
	if got := normalizedStatusCode(999); got != "other" {
		t.Errorf("invalid status code = %q", got)
	}
}
