package api

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeHTTPServersStopsOnLifecycleCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handlerCalled := make(chan struct{}, 1)
	server := newHTTPServer(listener.Addr().String(), http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		select {
		case handlerCalled <- struct{}{}:
		default:
		}
		_, _ = io.WriteString(writer, "ok")
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveHTTPServers(ctx, []httpServerBinding{{name: "test", server: server, listener: listener}})
	}()

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	select {
	case <-handlerCalled:
	case <-time.After(time.Second):
		t.Fatal("HTTP server did not handle a request")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTPServers() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP lifecycle did not stop after cancellation")
	}
}
