// Package dnsrecord resolves the experimental IPv8 TXT convention.
package dnsrecord

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/mryuvraj1/ipv8/internal/core"
)

const lookupTimeout = 5 * time.Second

var (
	ErrDomain      = errors.New("invalid domain name")
	ErrNotFound    = errors.New("IPv8 DNS record not found")
	ErrInvalid     = errors.New("invalid IPv8 DNS record")
	ErrUnsupported = errors.New("unsupported IPv8 DNS record version")
	ErrExpired     = errors.New("IPv8 DNS record expired")
	ErrLookup      = errors.New("DNS lookup failed")
)

type Record struct {
	Domain    string     `json:"domain"`
	TXTName   string     `json:"txtName"`
	Address   string     `json:"address"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CheckedAt time.Time  `json:"checkedAt"`
}

type TXTLookup func(context.Context, string) ([]string, error)

// ValidateDomain accepts ASCII DNS names, including IDNA A-labels. A final
// root dot is optional; IP literals, empty labels, and invalid host labels fail.
func ValidateDomain(domain string) (string, error) {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")
	if len(domain) == 0 || len(domain) > 253 || net.ParseIP(domain) != nil {
		return "", ErrDomain
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return "", ErrDomain
	}
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrDomain
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return "", ErrDomain
			}
		}
	}
	return strings.ToLower(domain), nil
}

// Parse accepts one v1 record and ignores unrelated TXT records. If an IPv8
// record is malformed, expired, duplicated, or from an unknown version, it
// fails closed rather than selecting another record.
func Parse(txt []string, now time.Time) (Record, error) {
	var found string
	for _, value := range txt {
		if !strings.HasPrefix(value, "v=ipv8") {
			continue
		}
		if found != "" {
			return Record{}, ErrInvalid
		}
		found = value
	}
	if found == "" {
		return Record{}, ErrNotFound
	}
	if !strings.HasPrefix(found, "v=ipv8-1;") {
		return Record{}, ErrUnsupported
	}
	fields := strings.Split(found, "; ")
	if len(fields) < 2 || len(fields) > 3 || fields[0] != "v=ipv8-1" || !strings.HasPrefix(fields[1], "address=") {
		return Record{}, ErrInvalid
	}
	address := strings.TrimPrefix(fields[1], "address=")
	parsed, err := core.ParseAddress(address)
	if err != nil || parsed.String() != address {
		return Record{}, ErrInvalid
	}
	record := Record{Address: address}
	if len(fields) == 3 {
		if !strings.HasPrefix(fields[2], "expires=") {
			return Record{}, ErrInvalid
		}
		raw := strings.TrimPrefix(fields[2], "expires=")
		expires, err := time.Parse("2006-01-02T15:04:05Z", raw)
		if err != nil || expires.UTC().Format("2006-01-02T15:04:05Z") != raw {
			return Record{}, ErrInvalid
		}
		record.ExpiresAt = &expires
		if !now.Before(expires) {
			return Record{}, ErrExpired
		}
	}
	return record, nil
}

// Resolve looks up _ipv8.<domain> with a bounded timeout. It leaves caching
// to the configured recursive resolver and does not maintain an app cache.
func Resolve(ctx context.Context, domain string, lookup TXTLookup, now time.Time) (Record, error) {
	domain, err := ValidateDomain(domain)
	if err != nil {
		return Record{}, err
	}
	if lookup == nil {
		lookup = func(ctx context.Context, name string) ([]string, error) {
			return net.DefaultResolver.LookupTXT(ctx, name)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	name := "_ipv8." + domain + "."
	values, err := lookup(ctx, name)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("%w: %v", ErrLookup, err)
	}
	record, err := Parse(values, now)
	if err != nil {
		return Record{}, err
	}
	record.Domain, record.TXTName, record.CheckedAt = domain, name, now.UTC()
	return record, nil
}
