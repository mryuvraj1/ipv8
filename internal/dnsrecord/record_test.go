package dnsrecord

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mryuvraj1/ipv8/internal/core"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func validAddress(t *testing.T) string {
	t.Helper()
	a, err := core.GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	return a.String()
}

func TestParseValidAndUnexpired(t *testing.T) {
	address := validAddress(t)
	expires := testNow.Add(time.Hour)
	value := "v=ipv8-1; address=" + address + "; expires=" + expires.Format("2006-01-02T15:04:05Z")
	record, err := Parse([]string{"unrelated", value}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if record.Address != address || record.ExpiresAt == nil || !record.ExpiresAt.Equal(expires) {
		t.Fatalf("unexpected parsed record: %#v", record)
	}
}

func TestParseMissingMalformedExpiredAndUnsupported(t *testing.T) {
	address := validAddress(t)
	cases := []struct {
		name string
		txt  []string
		want error
	}{
		{"missing", []string{"google-site-verification=abc"}, ErrNotFound},
		{"empty", nil, ErrNotFound},
		{"bad-address", []string{"v=ipv8-1; address=ipv8:bad"}, ErrInvalid},
		{"bad-fields", []string{"v=ipv8-1; address=" + address + "; extra=yes"}, ErrInvalid},
		{"bad-expiry", []string{"v=ipv8-1; address=" + address + "; expires=tomorrow"}, ErrInvalid},
		{"duplicate", []string{"v=ipv8-1; address=" + address, "v=ipv8-1; address=" + address}, ErrInvalid},
		{"unsupported", []string{"v=ipv8-2; address=" + address}, ErrUnsupported},
		{"expired", []string{"v=ipv8-1; address=" + address + "; expires=" + testNow.Format("2006-01-02T15:04:05Z")}, ErrExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.txt, testNow)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Parse error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateDomain(t *testing.T) {
	for _, domain := range []string{"example.com", "node.example.co.uk", "xn--bcher-kva.example", "EXAMPLE.COM."} {
		if _, err := ValidateDomain(domain); err != nil {
			t.Errorf("ValidateDomain(%q): %v", domain, err)
		}
	}
	for _, domain := range []string{"", "localhost", "127.0.0.1", "bad..example", "-bad.example", "bad-.example", "a_b.example", "https://example.com", "a." + strings.Repeat("b", 64) + ".com"} {
		if _, err := ValidateDomain(domain); !errors.Is(err, ErrDomain) {
			t.Errorf("ValidateDomain(%q) error = %v, want ErrDomain", domain, err)
		}
	}
}

func TestResolveAndLookupFailures(t *testing.T) {
	address := validAddress(t)
	lookup := func(_ context.Context, name string) ([]string, error) {
		if name != "_ipv8.example.com." {
			t.Fatalf("lookup name = %q", name)
		}
		return []string{"v=ipv8-1; address=" + address}, nil
	}
	record, err := Resolve(context.Background(), "Example.COM.", lookup, testNow)
	if err != nil || record.Address != address || record.Domain != "example.com" || record.TXTName != "_ipv8.example.com." || !record.CheckedAt.Equal(testNow) {
		t.Fatalf("Resolve = %#v, %v", record, err)
	}

	_, err = Resolve(context.Background(), "example.com", func(context.Context, string) ([]string, error) {
		return nil, &net.DNSError{Err: "no such host", Name: "_ipv8.example.com", IsNotFound: true}
	}, testNow)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing DNS result error = %v", err)
	}
	_, err = Resolve(context.Background(), "example.com", func(context.Context, string) ([]string, error) {
		return nil, errors.New("resolver unavailable")
	}, testNow)
	if !errors.Is(err, ErrLookup) {
		t.Fatalf("resolver failure error = %v", err)
	}
}
