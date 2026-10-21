# Security policy

## Reporting a vulnerability

Please do not open public issues for security problems. Email **ymohit799057@gmail.com** with a description, affected versions and, if possible, a proof of concept. You will get an acknowledgement within three working days.

## In scope

- The `capledger` agent and anything that runs on GPU hosts.
- Evidence-log integrity: ways to alter, remove or forge entries without `evidence verify` noticing (other than the limits documented in `docs/evidence-format.md`).
- SLA engine determinism: inputs that produce different statements on different machines.
- Key handling.

## Supported versions

Only the latest release receives fixes during the pre-1.0 period.

See `docs/threat-model.md` for the assumptions behind the design.
