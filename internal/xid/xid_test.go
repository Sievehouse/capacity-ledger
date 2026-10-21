package xid

import (
	"bytes"
	"os"
	"testing"
)

func TestParseLineFormats(t *testing.T) {
	cases := []struct {
		line   string
		kind   string
		code   int
		device string
		ts     string
	}{
		{"6,1234,5678901,-;NVRM: Xid (PCI:0000:3b:00): 79, pid=0, GPU has fallen off the bus.", "xid", 79, "0000:3b:00", ""},
		{"2026-10-03T04:11:58+0000 gpu-003 kernel: NVRM: Xid (PCI:0000:3b:00): 48, pid=1234, name=python3, An uncorrectable double bit error (DBE) has been detected.", "xid", 48, "0000:3b:00", "2026-10-03T04:11:58Z"},
		{"2026-10-03T09:41:58+05:30 gpu-003 kernel: NVRM: Xid (PCI:0000:5d:00): 94, pid=48211, name=python3, Contained ECC error.", "xid", 94, "0000:5d:00", "2026-10-03T04:11:58Z"},
		{"[Sat Oct  3 04:15:02 2026] nvidia-nvswitch2: SXid (PCI:0000:07:00.0): 12028, Non-fatal, Link 30 egress error", "sxid", 12028, "0000:07:00.0", ""},
	}
	for _, c := range cases {
		o, ok := ParseLine(c.line)
		if !ok {
			t.Fatalf("not parsed: %s", c.line)
		}
		if o.Kind != c.kind || o.Code != c.code || o.Device != c.device || o.TS != c.ts {
			t.Fatalf("line %q parsed as %+v", c.line, o)
		}
	}
	if _, ok := ParseLine("mlx5_core 0000:1a:00.0: Port module event: Cable plugged"); ok {
		t.Fatal("non-XID line must not parse")
	}
}

func TestDefaultPolicyClassifies(t *testing.T) {
	p, err := DefaultPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Classify(Observation{Kind: "xid", Code: 79}).Action; got != NodeDown {
		t.Fatalf("xid 79 -> %s, want node_down", got)
	}
	if got := p.Classify(Observation{Kind: "xid", Code: 94}).Action; got != App {
		t.Fatalf("xid 94 -> %s, want app", got)
	}
	if got := p.Classify(Observation{Kind: "xid", Code: 9999}).Action; got != Review {
		t.Fatalf("unknown xid -> %s, want review", got)
	}
	if got := p.Classify(Observation{Kind: "sxid", Code: 12028}).Action; got != Review {
		t.Fatalf("sxid -> %s, want review", got)
	}
}

func TestExamplePolicyMatchesEmbedded(t *testing.T) {
	b, err := os.ReadFile("../../examples/policies/xid-default.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, DefaultPolicyJSON()) {
		t.Fatal("examples/policies/xid-default.json differs from the embedded default policy; copy internal/xid/default_policy.json over it")
	}
}

func TestPolicyRejectsUnknownAction(t *testing.T) {
	_, err := ParsePolicy([]byte(`{"version":"x","xid":{"79":{"action":"panic"}},"default":{"action":"review"}}`))
	if err == nil {
		t.Fatal("expected an error for an unknown action")
	}
}
