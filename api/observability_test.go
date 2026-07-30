package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPublicMuxDoesNotExposeObservability(t *testing.T) {
	mux := http.NewServeMux()
	handlersRegister(mux)

	for _, path := range []string{"/metrics", "/-/healthy", "/-/ready"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			mux.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("public %s status = %d; want 404", path, recorder.Code)
			}
		})
	}
}

func TestNewHTTPServerLimits(t *testing.T) {
	server := newHTTPServer("127.0.0.1:8080", http.NotFoundHandler())
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 15*time.Second {
		t.Errorf("ReadTimeout = %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 30*time.Second {
		t.Errorf("WriteTimeout = %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout = %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != 64*1024 {
		t.Errorf("MaxHeaderBytes = %d", server.MaxHeaderBytes)
	}
}
