package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/mryuvraj1/ipv8/internal/core"
)

func TestAdminDNSVerification(t *testing.T) {
	a, err := New(filepath.Join(t.TempDir(), "ipv8.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	password := testPassword(t)
	if err := a.BootstrapAdmin("admin@example.com", password); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()

	admin := newClient(t)
	login := request(t, admin, server.URL, "POST", "/api/admin/login", map[string]string{"email": "admin@example.com", "password": password})
	if login.Code != http.StatusOK {
		t.Fatalf("admin login: %d %s", login.Code, login.Body)
	}
	user := newClient(t)
	register := request(t, user, server.URL, "POST", "/api/register", map[string]string{"email": "user@example.com", "password": testPassword(t)})
	if register.Code != http.StatusOK {
		t.Fatalf("user registration: %d %s", register.Code, register.Body)
	}
	address, err := core.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]string{"domain": "Example.com.", "address": address.String()}
	if got := request(t, newClient(t), server.URL, "POST", "/api/admin/dns/verify", input).Code; got != http.StatusUnauthorized {
		t.Fatalf("anonymous verify status = %d", got)
	}
	if got := request(t, user, server.URL, "POST", "/api/admin/dns/verify", input).Code; got != http.StatusForbidden {
		t.Fatalf("user verify status = %d", got)
	}
	a.dnsLookup = func(_ context.Context, name string) ([]string, error) {
		if name != "_ipv8.example.com." {
			t.Fatalf("DNS query name = %q", name)
		}
		return []string{"v=ipv8-1; address=" + address.String()}, nil
	}
	verified := request(t, admin, server.URL, "POST", "/api/admin/dns/verify", input)
	var response struct {
		Verified bool `json:"verified"`
		Record   struct {
			Address string `json:"address"`
			TXTName string `json:"txtName"`
		} `json:"record"`
	}
	if err := json.Unmarshal(verified.Body.Bytes(), &response); err != nil || verified.Code != http.StatusOK || !response.Verified || response.Record.Address != address.String() || response.Record.TXTName != "_ipv8.example.com." {
		t.Fatalf("verify response: %d %s (%v)", verified.Code, verified.Body, err)
	}

	other, err := core.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	input["address"] = other.String()
	mismatch := request(t, admin, server.URL, "POST", "/api/admin/dns/verify", input)
	if mismatch.Code != http.StatusOK || !containsJSONFalse(mismatch.Body.Bytes(), "verified") {
		t.Fatalf("mismatch response: %d %s", mismatch.Code, mismatch.Body)
	}
	input["domain"] = "not a domain"
	if got := request(t, admin, server.URL, "POST", "/api/admin/dns/verify", input).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid domain status = %d", got)
	}
}

func containsJSONFalse(data []byte, key string) bool {
	var v map[string]any
	return json.Unmarshal(data, &v) == nil && v[key] == false
}
