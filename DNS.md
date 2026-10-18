# IPv8 DNS metadata

IPv8 can publish an experimental IPv8 address as DNS TXT metadata. This does not define a new DNS record type, alter conventional DNS resolution, or create a transport route to the address.

## Publish a record

In the DNS zone for `example.com`, add one TXT record:

| Field | Value |
| --- | --- |
| Type | `TXT` |
| Name | `_ipv8` (the fully qualified owner name is `_ipv8.example.com`) |
| Value | `v=ipv8-1; address=ipv8:YOUR_CANONICAL_ADDRESS` |

Replace the placeholder with the complete canonical 256-bit IPv8 address shown in the account panel. An optional expiration can be appended in UTC:

```text
v=ipv8-1; address=ipv8:AEBA-GBAF-AYDQ-QCIK-BMGA-2DQP-CAIR-EEYU-CULB-OGAZ-DINR-YHI6-D4QA; expires=2027-01-01T00:00:00Z
```

The date above is illustrative. The record parser accepts exactly one version 1 record per queried name. Unrelated TXT values are ignored; duplicate IPv8 records, malformed values, noncanonical addresses, unsupported versions, and expired records are rejected. Without `expires`, the record has no application-level expiration. Set an appropriate DNS TTL in the provider; replacements can remain cached until resolvers' TTLs expire.

## Verify in the panel

An administrator can open **Nodes → Verify an IPv8 DNS mapping**, enter the domain and expected IPv8 address, and query its `_ipv8.<domain>` TXT record. The API performs a bounded DNS lookup, validates the address and optional expiry, and returns whether the published address matches the expected one. It does not persist the verification result. The admin endpoint is rate limited.

The Go standard resolver does not expose TXT TTL values, so IPv8 keeps no application cache and leaves DNS caching to the configured recursive resolver and operating system. A verification result describes the DNS answer observed at its check time; it can become stale when the record changes or its TTL expires.

## Trust and reachability limits

DNS control only demonstrates that the queried DNS zone currently publishes the value. IPv8 does not cryptographically sign these records or bind them to an account, key, or legal identity. A DNS owner can change or remove the record, and DNSSEC validation is not independently enforced by the application. A matching TXT record is name-to-address metadata only. It does not prove global uniqueness, ownership of the IPv8 identifier, or that a destination is reachable. This project has no peer transport, routing, or public packet delivery.
