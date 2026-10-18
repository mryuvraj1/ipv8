package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	packetMagic               = "IPV8"
	packetVersion             = 2
	packetSourceOffset        = 6
	packetDestinationOffset   = packetSourceOffset + AddressLen
	packetPayloadLengthOffset = packetDestinationOffset + AddressLen
	packetChecksumOffset      = packetPayloadLengthOffset + 2
	PacketHeaderLen           = packetChecksumOffset + sha256.Size
	MaxPayload                = 1200
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
	b[4] = packetVersion
	copy(b[packetSourceOffset:packetDestinationOffset], p.Source[:])
	copy(b[packetDestinationOffset:packetPayloadLengthOffset], p.Destination[:])
	binary.BigEndian.PutUint16(b[packetPayloadLengthOffset:packetChecksumOffset], uint16(len(p.Payload)))
	copy(b[PacketHeaderLen:], p.Payload)
	sum := sha256.Sum256(append(b[:packetChecksumOffset:packetChecksumOffset], p.Payload...))
	copy(b[packetChecksumOffset:PacketHeaderLen], sum[:])
	return b, nil
}

func UnmarshalPacket(b []byte) (Packet, error) {
	var p Packet
	if len(b) < PacketHeaderLen || len(b) > PacketHeaderLen+MaxPayload || string(b[:4]) != packetMagic || b[4] != packetVersion || b[5] != 0 {
		return p, ErrPacket
	}
	n := int(binary.BigEndian.Uint16(b[packetPayloadLengthOffset:packetChecksumOffset]))
	if n > MaxPayload || len(b) != PacketHeaderLen+n {
		return p, ErrPacket
	}
	sum := sha256.Sum256(append(b[:packetChecksumOffset:packetChecksumOffset], b[PacketHeaderLen:]...))
	if !bytes.Equal(sum[:], b[packetChecksumOffset:PacketHeaderLen]) {
		return p, ErrPacket
	}
	copy(p.Source[:], b[packetSourceOffset:packetDestinationOffset])
	copy(p.Destination[:], b[packetDestinationOffset:packetPayloadLengthOffset])
	p.Payload = append([]byte(nil), b[PacketHeaderLen:]...)
	if p.Source.Reserved() || p.Destination.Reserved() {
		return Packet{}, ErrPacket
	}
	return p, nil
}
