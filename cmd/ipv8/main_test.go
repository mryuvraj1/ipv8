package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestEnrollmentConfigAndAuthenticatedHeartbeat(t *testing.T) {
	var enrollments, heartbeats atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/installations/enroll":
			var in struct{ Token, Name, Platform, Version string }
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Token != "single-use-token" || in.Name != "test-host" || in.Platform == "" || in.Version != version {
				t.Errorf("invalid enrollment request: %#v, %v", in, err)
				http.Error(w, "invalid enrollment", http.StatusBadRequest)
				return
			}
			enrollments.Add(1)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "installation-id", "secret": "installation-secret"})
		case "/api/agent/heartbeat":
			var in struct{ ID, Secret string }
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID != "installation-id" || in.Secret != "installation-secret" {
				t.Errorf("invalid heartbeat credentials: %#v, %v", in, err)
				http.Error(w, "invalid heartbeat", http.StatusUnauthorized)
				return
			}
			heartbeats.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := filepath.Join(t.TempDir(), "agent.json")
	if err := enroll([]string{"--server", server.URL, "--token", "single-use-token", "--name", "test-host", "--config", config}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(config)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("enrollment config permissions: %v, %v", info, err)
	}
	cfg, err := readAgentConfig(config)
	if err != nil || cfg.ID != "installation-id" || cfg.Secret != "installation-secret" {
		t.Fatalf("saved config: %#v, %v", cfg, err)
	}
	if err := heartbeat(context.Background(), agentHTTPClient(time.Second), cfg); err != nil {
		t.Fatalf("agent heartbeat: %v", err)
	}
	if enrollments.Load() != 1 || heartbeats.Load() != 1 {
		t.Fatalf("enrollment/heartbeat counts = %d/%d", enrollments.Load(), heartbeats.Load())
	}
	if err := enroll([]string{"--server", server.URL, "--token", "another-token", "--name", "test-host", "--config", config}); err == nil {
		t.Fatal("enrollment overwrote an existing credential file")
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

func TestAgentServiceUsesTransientCredentialsAndNoCapabilities(t *testing.T) {
	unit, err := os.ReadFile(filepath.Join("..", "..", "packaging", "ipv8-agent.service"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(unit)
	for _, required := range []string{"DynamicUser=yes", "LoadCredential=agent.json:/etc/ipv8/agent.json", "--config %d/agent.json", "CapabilityBoundingSet="} {
		if !strings.Contains(text, required) {
			t.Errorf("service unit is missing %q", required)
		}
	}
	if strings.Contains(text, "User=root") || strings.Contains(text, "ReadWritePaths=") {
		t.Fatal("agent service unnecessarily runs as root or requests writable paths")
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
