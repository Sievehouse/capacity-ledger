# Evidence log format

An evidence log is a JSON Lines file. Each line is one entry:

```json
{"seq":2,"ts":"2026-10-21T21:43:10.123Z","kind":"event","payload":{"cause":"xid:79","id":"7acb4797ce3869f0-2","node":"gpu-003","seq":2,"source":"capledger-agent","ts":"2026-10-03T04:11:58Z","type":"down"},"prev":"4f3b…","keyId":"7acb4797ce3869f0","hash":"c08e…","sig":"base64…"}
```

| Field | Meaning |
| --- | --- |
| `seq` | 1, 2, 3 … with no gaps |
| `ts` | When the entry was written (RFC 3339, UTC); event times live in the payload |
| `kind` | `observation`, `event`, `statement`, `test-result`, `acceptance-report`, … |
| `payload` | Canonical JSON of the recorded object |
| `prev` | `hash` of the previous entry; 64 zeros for the first |
| `keyId` | First 16 hex characters of SHA-256 over the Ed25519 public key |
| `hash` | SHA-256 of `"capacity-ledger/evidence/v1\n"` followed by the canonical JSON of `{seq, ts, kind, payload, prev, keyId}` |
| `sig` | Base64 Ed25519 signature over the 32 raw bytes of `hash` |

## Canonical JSON

Object keys sorted, no insignificant whitespace, no HTML escaping, numbers exactly as written. Payloads avoid floating point where exactness matters; money and percentages are decimal strings.

## Verification

`capledger evidence verify --log L --pub K` checks, for every entry: the sequence has no gaps, `prev` matches the previous entry's hash, the stored hash matches the content, and the signature is valid for a trusted key. It reports every problem it finds, plus the Merkle root over all entry hashes.

Detected: edited payloads or fields, deleted or reordered entries, entries signed by unknown keys, truncated chains that are then extended.
Not detected by the log alone: deleting the *last* entries (compare the last hash or Merkle root against a copy held by the other party), or a writer who signs false observations with a valid key (see the threat model).

## Merkle root

Leaves are the raw entry hashes. The tree follows RFC 6962: leaf hash = SHA-256(0x00 ‖ leaf), node hash = SHA-256(0x01 ‖ left ‖ right), split at the largest power of two below n. Publishing the root to the counterparty each day (or embedding it in a statement) pins the whole history.

## Keys

`capledger keygen` writes `capledger.key.pem` (PKCS#8, mode 0600) and `capledger.pub.pem` (PKIX). Share the public key with counterparties; never commit private keys. Use one key per writer where possible, rotate by starting a new log with a new key, and keep old public keys for verifying history.
