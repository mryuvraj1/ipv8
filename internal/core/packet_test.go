package core

import (
	"bytes"
	"testing"
)

func TestPacketRoundTripAndRejectMalformed(t *testing.T) {
	s, _ := GenerateAddress()
	d, _ := GenerateAddress()
	p := Packet{Source: s, Destination: d, Payload: []byte("loopback proof")}
	b, err := p.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != PacketHeaderLen+len(p.Payload) || PacketHeaderLen != 104 {
		t.Fatalf("packet size/header = %d/%d, want payload+104/104", len(b), PacketHeaderLen)
	}
	got, err := UnmarshalPacket(b)
	if err != nil || got.Source != s || got.Destination != d || !bytes.Equal(got.Payload, p.Payload) {
		t.Fatalf("decode: %#v %v", got, err)
	}
	for _, bad := range [][]byte{nil, b[:len(b)-1], append(append([]byte(nil), b...), 0)} {
		if _, err := UnmarshalPacket(bad); err == nil {
			t.Error("accepted malformed packet")
		}
	}
	oldVersion := append([]byte(nil), b...)
	oldVersion[4] = 1
	if _, err := UnmarshalPacket(oldVersion); err == nil {
		t.Fatal("accepted packet with legacy 128-bit address version")
	}
	b[PacketHeaderLen] ^= 1
	if _, err := UnmarshalPacket(b); err == nil {
		t.Fatal("accepted packet with corrupt checksum")
	}
}

func TestPacketPayloadLimit(t *testing.T) {
	a, _ := GenerateAddress()
	if _, err := (Packet{Source: a, Destination: a, Payload: make([]byte, MaxPayload+1)}).MarshalBinary(); err == nil {
		t.Fatal("accepted oversized payload")
	}
}
