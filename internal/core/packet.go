package core

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	packetMagic     = "IPV8"
	PacketHeaderLen = 72
	MaxPayload      = 1200
)

var ErrPacket = errors.New("invalid IPv8 experimental packet")

type Packet struct {
	Source      Address
	Destination Address
	Payload     []byte
}

func (p Packet) MarshalBinary() ([]byte, error) {
	if p.Source.Reserved() || p.Destination.Reserved() || len(p.Payload) > MaxPayload {
		return nil, ErrPacket
	}
	b := make([]byte, PacketHeaderLen+len(p.Payload))
	copy(b[:4], packetMagic)
	b[4] = 1
	copy(b[6:22], p.Source[:])
	copy(b[22:38], p.Destination[:])
	binary.BigEndian.PutUint16(b[38:40], uint16(len(p.Payload)))
	copy(b[PacketHeaderLen:], p.Payload)
	sum := sha256.Sum256(append(b[:40:40], p.Payload...))
	copy(b[40:PacketHeaderLen], sum[:])
	return b, nil
}

func UnmarshalPacket(b []byte) (Packet, error) {
	var p Packet
	if len(b) < PacketHeaderLen || len(b) > PacketHeaderLen+MaxPayload || string(b[:4]) != packetMagic || b[4] != 1 || b[5] != 0 {
		return p, ErrPacket
	}
	n := int(binary.BigEndian.Uint16(b[38:40]))
	if n > MaxPayload || len(b) != PacketHeaderLen+n {
		return p, ErrPacket
	}
	sum := sha256.Sum256(append(b[:40:40], b[PacketHeaderLen:]...))
	if string(sum[:]) != string(b[40:PacketHeaderLen]) {
		return p, ErrPacket
	}
	copy(p.Source[:], b[6:22])
	copy(p.Destination[:], b[22:38])
	p.Payload = append([]byte(nil), b[PacketHeaderLen:]...)
	if p.Source.Reserved() || p.Destination.Reserved() {
		return Packet{}, ErrPacket
	}
	return p, nil
}
