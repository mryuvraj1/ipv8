package core

import (
	"strings"
	"testing"
)

func TestAddressRoundTripAndValidation(t *testing.T) {
	for _, seed := range []byte{0x01, 0x7f, 0x80, 0xfe} {
		var original Address
		for i := range original {
			original[i] = byte(i*37) ^ seed
		}
		if original.Reserved() {
			t.Fatal("test vector unexpectedly reserved")
		}
		text := original.String()
		if len(text) != 69 {
			t.Fatalf("address length = %d, want 69", len(text))
		}
		groups := strings.Split(strings.TrimPrefix(text, AddressPrefix), "-")
		if len(groups) != AddressGroupCount {
			t.Fatalf("group count = %d", len(groups))
		}
		for _, group := range groups {
			if len(group) != AddressGroupSize {
				t.Fatalf("group %q has length %d", group, len(group))
			}
		}
		got, err := ParseAddress(text)
		if err != nil || got != original {
			t.Fatalf("round trip: %x != %x: %v", got, original, err)
		}
	}
}

func TestAddressParsingRejectsMalformedAndNoncanonicalValues(t *testing.T) {
	var a Address
	a[0] = 0x01
	valid := a.String()
	invalidPadding := valid[:len(valid)-1] + "Z"
	badGroup := valid[:len(AddressPrefix)+3] + "--" + valid[len(AddressPrefix)+4:]
	badCharacter := valid[:len(valid)-1] + "0"
	lowercaseBody := valid[:len(AddressPrefix)] + strings.ToLower(valid[len(AddressPrefix):])
	for _, bad := range []string{
		valid + "A",
		strings.ToUpper(valid),
		"ipv8:1:" + valid[len(AddressPrefix):],
		"ipv8:" + strings.Repeat("A", AddressBodyLen),
		badGroup,
		badCharacter,
		lowercaseBody,
		invalidPadding,
	} {
		if _, err := ParseAddress(bad); err == nil {
			t.Errorf("accepted malformed address %q", bad)
		}
	}

	var zero, ones Address
	for i := range ones {
		ones[i] = 0xff
	}
	if _, err := ParseAddress(zero.String()); err == nil {
		t.Fatal("accepted reserved all-zero value")
	}
	if _, err := ParseAddress(ones.String()); err == nil {
		t.Fatal("accepted reserved all-ones value")
	}
}

func TestAddressGenerationUsesFull256BitSpace(t *testing.T) {
	a, err := GenerateAddress()
	if err != nil {
		t.Fatal(err)
	}
	if a.Reserved() || AddressBits != 256 || len(a) != 32 {
		t.Fatalf("generated address does not use a valid 256-bit value: %x", a)
	}
	if strings.HasPrefix(a.String(), AddressPrefix+"1:") {
		t.Fatal("legacy versioned 128-bit address format remains in use")
	}
}
