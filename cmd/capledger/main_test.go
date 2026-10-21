package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func capledger(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(""), &out, &errOut)
	return out.String() + errOut.String(), code
}

func TestComputeThenReplay(t *testing.T) {
	dir := t.TempDir()
	stmt := filepath.Join(dir, "statement.json")
	contractPath := "../../examples/contracts/hgx-16-node.json"
	eventsPath := "../../examples/events/hgx-16-node-2026-10.jsonl"

	out, code := capledger(t, "sla", "compute", "--contract", contractPath, "--events", eventsPath, "--period", "2026-10", "--out", stmt, "--format", "text")
	if code != 0 || !strings.Contains(out, "Availability  99.7021%") || !strings.Contains(out, "15000.00 USD") {
		t.Fatalf("compute failed (%d):\n%s", code, out)
	}
	if out, code := capledger(t, "sla", "verify", "--statement", stmt, "--contract", contractPath, "--events", eventsPath); code != 0 {
		t.Fatalf("replay failed (%d): %s", code, out)
	}
	b, _ := os.ReadFile(stmt)
	tampered := filepath.Join(dir, "tampered.json")
	os.WriteFile(tampered, bytes.Replace(b, []byte(`"percent": 5`), []byte(`"percent": 0`), 1), 0o644)
	if _, code := capledger(t, "sla", "verify", "--statement", tampered); code != 1 {
		t.Fatalf("tampered statement should fail its seal check with exit code 1, got %d", code)
	}
}

func TestEvidenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keys := filepath.Join(dir, "keys")
	logPath := filepath.Join(dir, "evidence.jsonl")
	priv, pub := filepath.Join(keys, "capledger.key.pem"), filepath.Join(keys, "capledger.pub.pem")

	if out, code := capledger(t, "keygen", "--out", keys); code != 0 {
		t.Fatalf("keygen: %s", out)
	}
	if out, code := capledger(t, "agent", "--node", "gpu-003", "--source", "../../examples/samples/kern.log", "--once", "--log", logPath, "--key", priv); code != 0 {
		t.Fatalf("agent: %s", out)
	}
	if out, code := capledger(t, "event", "emit", "--log", logPath, "--key", priv, "--node", "gpu-003", "--type", "up", "--ts", "2026-10-03T06:40:00Z", "--cause", "repaired"); code != 0 {
		t.Fatalf("event emit: %s", out)
	}
	if out, code := capledger(t, "evidence", "verify", "--log", logPath, "--pub", pub); code != 0 || !strings.Contains(out, `"ok": true`) {
		t.Fatalf("verify: %s", out)
	}
	out, code := capledger(t, "events", "export", "--log", logPath, "--pub", pub)
	if code != 0 {
		t.Fatalf("export: %s", out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"type":"down"`) || !strings.Contains(lines[1], `"type":"up"`) {
		t.Fatalf("expected a down and an up event, got:\n%s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	if _, code := capledger(t, "frobnicate"); code != 2 {
		t.Fatalf("unknown command should exit 2, got %d", code)
	}
}
