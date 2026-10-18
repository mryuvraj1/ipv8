package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestReadAgentConfigRequiresPrivateRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.json")
	data := []byte(`{"server":"http://127.0.0.1:8080","id":"id","secret":"secret"}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := readAgentConfig(path); err != nil || cfg.Secret != "secret" {
		t.Fatalf("read private config: %#v %v", cfg, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentConfig(path); err == nil {
		t.Fatal("accepted group/world-readable credentials")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "agent-link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readAgentConfig(link); err == nil {
		t.Fatal("accepted symlinked credentials")
	}
	if _, err := readAgentConfig(dir); err == nil {
		t.Fatal("accepted a directory as agent configuration")
	}
}

func TestValidServerRequiresSecureNonLocalOrigin(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://example.com/path", "https://user:pass@example.com", "ftp://example.com"} {
		if _, err := validServer(raw); err == nil {
			t.Errorf("accepted invalid server URL %q", raw)
		}
	}
	if _, err := validServer("http://localhost:8080/"); err != nil {
		t.Fatalf("rejected localhost prototype URL: %v", err)
	}
}
