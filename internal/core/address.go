package core

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
)

const AddressPrefix = "ipv8:1:"

var (
	addressEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)
	ErrAddress      = errors.New("invalid IPv8 experimental address")
)

// Address is a 128-bit experimental identifier. It has no public routing meaning.
type Address [16]byte

func GenerateAddress() (Address, error) {
	for {
		var a Address
		if _, err := rand.Read(a[:]); err != nil {
			return Address{}, err
		}
		if !a.Reserved() {
			return a, nil
		}
	}
}

func ParseAddress(s string) (Address, error) {
	var a Address
	if !strings.HasPrefix(s, AddressPrefix) || len(s) != len(AddressPrefix)+26 {
		return a, ErrAddress
	}
	body := s[len(AddressPrefix):]
	if strings.ToLower(body) != body {
		return a, ErrAddress
	}
	b, err := addressEncoding.DecodeString(strings.ToUpper(body))
	if err != nil || len(b) != len(a) {
		return a, ErrAddress
	}
	copy(a[:], b)
	if a.Reserved() || a.String() != s {
		return Address{}, ErrAddress
	}
	return a, nil
}

func (a Address) String() string {
	return AddressPrefix + strings.ToLower(addressEncoding.EncodeToString(a[:]))
}

func (a Address) Reserved() bool {
	zero, ones := true, true
	for _, b := range a {
		zero = zero && b == 0
		ones = ones && b == 0xff
	}
	return zero || ones
}
