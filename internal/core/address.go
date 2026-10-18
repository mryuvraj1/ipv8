package core

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"
)

const AddressPrefix = "ipv8:"
const (
	AddressBits       = 256
	AddressLen        = AddressBits / 8
	AddressBodyLen    = 52
	AddressGroupSize  = 4
	AddressGroupCount = 13
)

var (
	addressEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)
	ErrAddress      = errors.New("invalid IPv8 experimental address")
)

// Address is a 256-bit experimental identifier with no public routing meaning.
type Address [AddressLen]byte

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
	if !strings.HasPrefix(s, AddressPrefix) || len(s) != len(AddressPrefix)+AddressBodyLen+AddressGroupCount-1 {
		return a, ErrAddress
	}
	groups := strings.Split(s[len(AddressPrefix):], "-")
	if len(groups) != AddressGroupCount {
		return a, ErrAddress
	}
	var body strings.Builder
	body.Grow(AddressBodyLen)
	for _, group := range groups {
		if len(group) != AddressGroupSize {
			return a, ErrAddress
		}
		body.WriteString(group)
	}
	b, err := addressEncoding.DecodeString(body.String())
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
	body := addressEncoding.EncodeToString(a[:])
	var grouped strings.Builder
	grouped.Grow(AddressBodyLen + AddressGroupCount - 1)
	for i := 0; i < len(body); i += AddressGroupSize {
		if i > 0 {
			grouped.WriteByte('-')
		}
		grouped.WriteString(body[i : i+AddressGroupSize])
	}
	return AddressPrefix + grouped.String()
}

func (a Address) Reserved() bool {
	zero, ones := true, true
	for _, b := range a {
		zero = zero && b == 0
		ones = ones && b == 0xff
	}
	return zero || ones
}
