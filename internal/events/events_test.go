package events

import (
	"strings"
	"testing"
)

func TestReadJSONLSortsAndNormalises(t *testing.T) {
	in := `# comment
{"id":"b","ts":"2026-10-03T06:40:00+05:30","node":"n1","type":"up"}
{"id":"a","ts":"2026-10-03T01:00:00.900Z","node":"n1","type":"down","cause":"xid:79"}

{"id":"c","ts":"2026-10-03T01:00:00Z","seq":-1,"node":"n2","type":"down"}
`
	evs, err := ReadJSONL(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d", len(evs))
	}
	// 06:40+05:30 is 01:10Z; ties at 01:00:00 are broken by seq.
	gotOrder := evs[0].ID + evs[1].ID + evs[2].ID
	if gotOrder != "cab" {
		t.Fatalf("order %q, want cab", gotOrder)
	}
	if evs[2].TS != "2026-10-03T01:10:00Z" {
		t.Fatalf("timestamp not normalised: %s", evs[2].TS)
	}
	if evs[1].TS != "2026-10-03T01:00:00Z" {
		t.Fatalf("sub-second part not truncated: %s", evs[1].TS)
	}
}

func TestRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"duplicate id":  `{"id":"a","ts":"2026-10-01T00:00:00Z","node":"n","type":"down"}` + "\n" + `{"id":"a","ts":"2026-10-01T00:00:01Z","node":"n","type":"up"}`,
		"unknown type":  `{"id":"a","ts":"2026-10-01T00:00:00Z","node":"n","type":"offline"}`,
		"unknown field": `{"id":"a","ts":"2026-10-01T00:00:00Z","node":"n","type":"down","severity":"high"}`,
		"bad ts":        `{"id":"a","ts":"yesterday","node":"n","type":"down"}`,
		"missing node":  `{"id":"a","ts":"2026-10-01T00:00:00Z","type":"down"}`,
	}
	for name, in := range cases {
		if _, err := ReadJSONL(strings.NewReader(in)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDigestIgnoresInputOrder(t *testing.T) {
	a := `{"id":"1","ts":"2026-10-01T00:00:00Z","node":"n","type":"down"}
{"id":"2","ts":"2026-10-01T01:00:00Z","node":"n","type":"up"}`
	b := `{"id":"2","ts":"2026-10-01T01:00:00Z","node":"n","type":"up"}
{"id":"1","ts":"2026-10-01T00:00:00Z","node":"n","type":"down"}`
	ea, _ := ReadJSONL(strings.NewReader(a))
	eb, _ := ReadJSONL(strings.NewReader(b))
	da, _ := Digest(ea)
	db, _ := Digest(eb)
	if da != db {
		t.Fatalf("digest depends on input order")
	}
}
