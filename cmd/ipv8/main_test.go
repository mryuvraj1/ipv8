package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentClientDoesNotForwardCredentialsOnRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	cfg := agentConfig{Server: redirect.URL, ID: "installation-id", Secret: "installation-secret"}
	err := heartbeat(context.Background(), agentHTTPClient(time.Second), cfg)
	if err == nil {
		t.Fatal("heartbeat unexpectedly accepted a redirect response")
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d request(s); credentials may have been forwarded", got)
	}
}
