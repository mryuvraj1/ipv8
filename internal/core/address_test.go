package core

import (
	"strings"
	"testing"
)

func TestAddressRoundTripAndValidation(t *testing.T) {
	a, err := GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	if len(a.String()) != 33 {
		t.Fatalf("address length = %d", len(a.String()))
	}
	got, err := ParseAddress(a.String())
	if err != nil || got != a {
		t.Fatalf("round trip: %v %v", got, err)
	}
	for _, bad := range []string{a.String() + "a", strings.ToUpper(a.String()), "ipv8:2:" + a.String()[7:], "ipv8:1:aaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, err := ParseAddress(bad); err == nil {
			t.Errorf("accepted invalid address %q", bad)
		}
	}
	if _, err := ParseAddress(AddressPrefix + strings.Repeat("a", 26)); err == nil {
		t.Fatal("accepted reserved all-zero value")
	}
	if _, err := ParseAddress(AddressPrefix + strings.Repeat("7", 25) + "4"); err == nil {
		t.Fatal("accepted reserved all-ones value")
	}
}
