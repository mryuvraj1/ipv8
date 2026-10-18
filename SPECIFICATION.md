# IPv8 Experimental Address and Packet Specification

Status: prototype specification, version 1. Nothing in this document defines a public Internet protocol or promises interoperability with other implementations.

## Address format

An IPv8 v1 address names one experimental endpoint identifier. It is not an IP address, hostname, public key, or route.

| Property | Value |
| --- | --- |
| Identifier length | 128 bits (16 bytes) |
| Text prefix | `ipv8:1:` |
| Encoding | RFC 4648 Base32 alphabet, no padding, rendered in lowercase |
| Text body | 26 ASCII characters (`a`–`z`, `2`–`7`) |
| Total text length | Exactly 33 ASCII characters |
| Entropy | 128 random bits before reserved values are rejected |

The body is the Base32 encoding of the 16 identifier bytes. The final Base32 character contains the last four identifier bits followed by one zero padding bit. Parsers reject non-canonical encodings by decoding and comparing with the canonical lowercase re-encoding. Uppercase, whitespace, padding, alternate versions, and trailing characters are invalid.

The all-zero identifier and the all-`0xff` identifier are reserved and invalid. There is no broadcast address. Generation reads from the operating system cryptographic random source and retries a reserved result.

## Uniqueness scope

The panel assigns at most one address to each user and stores it in its configured JSON database. While assigning an address, the process checks all stored user addresses under its database mutex and retries on collision. The uniqueness guarantee is therefore limited to one intact database operated by one IPv8 server process. It does not coordinate across separate installations, copied databases, or independently operated servers. Addresses persist across restarts with the database.

The address space has `2^128 - 2` assignable identifiers under the reserved-value rule. The generator does not claim mathematically guaranteed global uniqueness.

## Packet format

The prototype packet is a binary envelope used by the local loopback simulator. All multibyte integer fields use network byte order. No packet transport, forwarding, public routing, authentication, or encryption is implemented.

| Offset | Size | Field | Rule |
| --- | ---: | --- | --- |
| 0 | 4 | Magic | ASCII `IPV8` |
| 4 | 1 | Version | `1` |
| 5 | 1 | Flags | `0`; all other values are rejected |
| 6 | 16 | Source | Raw address identifier; reserved values rejected |
| 22 | 16 | Destination | Raw address identifier; reserved values rejected |
| 38 | 2 | Payload length | Unsigned 16-bit big-endian; 0 through 1200 |
| 40 | 32 | Checksum | SHA-256 of bytes `[0,40)` concatenated with payload bytes |
| 72 | 0–1200 | Payload | Opaque application bytes |

The fixed header is 72 bytes; maximum packet size is 1272 bytes. Decoding rejects invalid magic, version, flags, reserved addresses, lengths, truncation, trailing bytes, and checksum mismatches before returning a packet. The checksum detects accidental corruption only; it does not authenticate a sender or resist deliberate modification.

## Implemented behavior and limits

The dashboard creates and validates addresses, then can send a UTF-8 payload to its own address through the local simulator. The simulator serializes the packet, decodes it again, and returns the validated payload in the same API process. It does not transmit packets over a socket. The agent only sends authenticated HTTPS status heartbeats to the panel; it does not carry IPv8 packets or change host routes/firewall settings.
