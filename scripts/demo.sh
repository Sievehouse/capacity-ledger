#!/usr/bin/env bash
# End-to-end demo on synthetic data. Needs Go 1.22+; uses a temporary directory.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WORK="${WORK:-$(mktemp -d)}"
cd "$ROOT"

step() { printf '\n==> %s\n' "$*"; }

step "Build capledger"
go build -o "$WORK/capledger" ./cmd/capledger
CL="$WORK/capledger"
"$CL" version

step "Create a signing key"
"$CL" keygen --out "$WORK/keys"
KEY="$WORK/keys/capledger.key.pem"
PUB="$WORK/keys/capledger.pub.pem"
LOG="$WORK/evidence.jsonl"

step "Replay a sample kernel log through the agent (one-shot mode)"
"$CL" agent --node gpu-003 --source examples/samples/kern.log --once --log "$LOG" --key "$KEY"

step "Record the repair that closes the incident (normally done by an integration)"
"$CL" event emit --log "$LOG" --key "$KEY" --node gpu-003 --type up \
  --ts 2026-10-03T06:40:00Z --cause node-reseated --source provider-health-check

step "Verify the evidence log (hash chain + signatures)"
"$CL" evidence verify --log "$LOG" --pub "$PUB"

step "Export the signed events for the SLA engine"
"$CL" events export --log "$LOG" --pub "$PUB" | tee "$WORK/agent-events.jsonl"

step "Compute the October statement for the 16-node example contract"
"$CL" sla compute --contract examples/contracts/hgx-16-node.json \
  --events examples/events/hgx-16-node-2026-10.jsonl --period 2026-10 \
  --out "$WORK/statement.json" --format text

step "Replay the statement independently (what the customer or an insurer would run)"
"$CL" sla verify --statement "$WORK/statement.json" \
  --contract examples/contracts/hgx-16-node.json --events examples/events/hgx-16-node-2026-10.jsonl

step "Seal the statement into the evidence log and verify again"
"$CL" evidence append --log "$LOG" --key "$KEY" --kind statement --payload "$WORK/statement.json"
"$CL" evidence verify --log "$LOG" --pub "$PUB" | grep -E '"(ok|entries|merkleRoot)"'

step "Rack-scale example: three NVL72 trays overlap, so the whole rack counts as down"
"$CL" sla compute --contract examples/contracts/nvl72-2-racks.json \
  --events examples/events/nvl72-2026-10.jsonl --period 2026-10 --format text | head -8

printf '\nDone. Files are in %s\n' "$WORK"
