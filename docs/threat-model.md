# Threat model

Capacity Ledger sits between parties with opposing financial interests: a provider who owes credits, a customer who receives them, and financiers who price risk on the result. The design assumes each party may try to shade the record in its favour, and that the agent itself is a new piece of software on high-value hosts.

## Assets

1. The integrity and completeness of evidence logs.
2. Signing keys.
3. The correctness and replayability of statements.
4. The GPU hosts the agent runs on.

## Threats and mitigations

| Threat | Mitigation in v0.1 | Residual risk and plan |
| --- | --- | --- |
| A party edits, deletes or reorders past entries | Hash chain, signatures, sequence numbers; `evidence verify` reports every break | Truncating the newest entries is invisible without an external copy. **Plan:** exchange daily Merkle roots between parties; hosted ledger with co-signing |
| The provider stops the agent or filters its input | Restarts cannot invent data; gaps are visible in timestamps | A silent gap is not yet flagged. **Plan:** heartbeat entries and gap detection; tenant-side probe as an independent second source |
| A provider-controlled agent records false data | Open-source agent; every observation stores the raw log line; policy version recorded | A root-level adversary on the host can lie. **Plan:** corroboration with tenant probes, GPU attestation and TPM host attestation where available |
| The customer injects false outages | Events carry their source and signing key; contracts can restrict accepted sources | **Plan:** contract-level allow-lists of sources and keys |
| Signing key theft | Private key files are mode 0600; one key per writer recommended | **Plan:** key rotation tooling, per-node keys by default, optional hardware-backed keys |
| Ambiguous contract terms exploited later | Spec rejects unknown fields and non-decimal numbers; statements fingerprint the exact contract | Free-text contract clauses remain outside the engine |
| Replays with a different engine give different results | Engine version recorded; mismatched replays are refused | Engine changes require a new version and a migration note |
| The agent becomes an attack path into GPU hosts | Read-only, no network listener, no third-party code, runs as non-root with all capabilities dropped (kern.log mode) | /dev/kmsg mode needs a privileged container; prefer the log-file mode |
| Sensitive data leaks through evidence | Only device counters, kernel fault lines and test results are recorded; no job contents, datasets or weights | Kernel lines can include process names; document and offer redaction |

## Out of scope for v0.1

Network transport of logs (logs are files; ship them with your existing tooling), multi-tenant hosting, and access control, all of which belong to the planned ledger service.
