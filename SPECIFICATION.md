# IPv8 Experimental Address and Packet Specification

Status: prototype specification, version 1. This is an experimental identifier and local packet envelope, not a public Internet protocol.

## Address format

An IPv8 address names one experimental endpoint identifier. It is not an IP address, hostname, public key, or route.

| Property | Value |
| --- | --- |
| Identifier size | Exactly 256 bits (32 bytes) |
| Text prefix | Exact lowercase `ipv8:` |
| Encoding | RFC 4648 Base32, alphabet `ABCDEFGHIJKLMNOPQRSTUVWXYZ234567`, no `=` padding |
| Encoded symbols | 52 uppercase ASCII characters |
| Grouping | 13 groups of exactly 4 symbols, joined with 12 hyphens |
| Total text length | Exactly 69 ASCII characters |
| Entropy | 256 random bits before reserved values are rejected |

Canonical text has this shape: `ipv8:AEBA-GBAF-AYDQ-QCIK-BMGA-2DQP-CAIR-EEYU-CULB-OGAZ-DINR-YHI6-D4QA` (the encoding of bytes `01` through `20` hexadecimal). The final group obeys the padding rule below.

Encoding consumes identifier bits most-significant-bit first. Since 256 is not divisible by 5, 52 Base32 symbols contain 260 bit positions: the final symbol carries the last address bit in its most significant position and has four zero padding bits. Therefore the final symbol must be `A` (`00000`) or `Q` (`10000`). A parser joins the groups, decodes exactly 32 bytes, rejects reserved values, and re-encodes the bytes; any text that differs from this canonical form is invalid. Lowercase body characters, uppercase prefix, whitespace, padding, missing/extra groups, incorrect group lengths, invalid alphabet symbols, nonzero unused bits, and trailing characters are rejected.

The all-zero 256-bit identifier and all-`0xff` identifier are reserved and invalid. There is no broadcast or multicast address. Generation reads from the operating system cryptographic random source and retries reserved results.

## Allocation, capacity, and uniqueness

The address space contains `2^256 - 2` assignable values after reserving the two endpoint values. A uniformly random generator has an approximate birthday-collision probability of `n(n-1) / 2^257` for `n` generated values; at one million values this is about `4.3 × 10^-66`. This calculation describes random collision risk only. It does not establish global uniqueness, identity, authenticity, or routing.

The panel stores at most one assigned address per user in its persistent JSON database. Under the database mutation lock it checks all existing assignments and retries on a collision before writing the new address. This enforces uniqueness within one intact database operated by one server process. Independently operated or copied databases do not coordinate. Existing addresses from the previous prototype format are replaced with newly generated 256-bit addresses at startup and persisted; valid canonical 256-bit addresses are preserved.

## DNS TXT metadata

IPv8 DNS metadata uses a conventional TXT record at `_ipv8.<domain>`. The record value is application-defined and versioned; it does not introduce a DNS RR type:

```text
v=ipv8-1; address=ipv8:<canonical-256-bit-address>
v=ipv8-1; address=ipv8:<canonical-256-bit-address>; expires=YYYY-MM-DDTHH:MM:SSZ
```

The second line is the optional-expiration form. Expiry is strict UTC whole seconds; the record is invalid at or after that instant. A verifier queries the absolute TXT owner `_ipv8.<lowercase-domain>.`, validates exactly one IPv8 record, parses the canonical address above, and compares it with the expected address. Unrelated TXT values are ignored. Duplicate IPv8 values, malformed fields, unsupported versions, invalid addresses, and expired values fail closed. The DNS provider's TTL is unchanged. The application keeps no DNS cache; it relies on the operating system/configured recursive resolver, while Go's standard TXT lookup does not expose TTL to the app.

DNS control only shows that a zone currently publishes a value. This format has no signature and is not independently bound to an IPv8 account, key, or identity. DNS metadata does not create a transport, route, or reachability guarantee. See [DNS.md](DNS.md) for provider instructions and the admin verification flow.

## Packet format

The prototype packet is a binary envelope used only by the local loopback simulator. All multibyte integer fields use network byte order. Packet version 2 carries the 256-bit address format. No packet transport, forwarding, public routing, sender authentication, or encryption is implemented.

| Offset | Size | Field | Rule |
| --- | ---: | --- | --- |
| 0 | 4 | Magic | ASCII `IPV8` |
| 4 | 1 | Version | `2` |
| 5 | 1 | Flags | `0`; all other values are rejected |
| 6 | 32 | Source | Raw address identifier; reserved values rejected |
| 38 | 32 | Destination | Raw address identifier; reserved values rejected |
| 70 | 2 | Payload length | Unsigned 16-bit big-endian; 0 through 1200 |
| 72 | 32 | Checksum | SHA-256 of bytes `[0,72)` concatenated with payload bytes |
| 104 | 0–1200 | Payload | Opaque application bytes |

The fixed header is 104 bytes; maximum packet size is 1304 bytes. Decoding rejects invalid magic, version, flags, reserved addresses, lengths, truncation, trailing bytes, and checksum mismatches before returning a packet. The checksum detects accidental corruption only; it does not authenticate a sender or resist deliberate modification.

## Implemented behavior and limits

The dashboard creates and validates addresses, then can send a UTF-8 payload to its own address through the local simulator. The simulator serializes the packet, decodes it again, and returns the validated payload in the same API process. It does not transmit packets over a socket. The Linux agent only sends authenticated HTTPS status heartbeats to the panel; it does not carry IPv8 packets or change host routes/firewall settings.
