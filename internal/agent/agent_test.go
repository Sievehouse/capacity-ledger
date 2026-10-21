package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"capledger/internal/events"
	"capledger/internal/evidence"
	"capledger/internal/xid"
)

func setup(t *testing.T) (*Agent, string, map[string]ed25519.PublicKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	w, err := evidence.OpenWriter(path, priv)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := xid.DefaultPolicy()
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{
		Node: "gpu-003", Policy: policy, Writer: w, Suppress: 10 * time.Minute,
		Now: func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return a, path, map[string]ed25519.PublicKey{evidence.KeyID(pub): pub}
}

func TestSampleKernelLog(t *testing.T) {
	a, path, keys := setup(t)
	f, err := os.Open("../../examples/samples/kern.log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := a.Run(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	st := a.Stats()
	// Two XID 79 lines (the second suppressed), one XID 94, one SXID.
	if st.Observations != 4 || st.Events != 1 || st.Suppressed != 1 {
		t.Fatalf("unexpected stats %+v", st)
	}
	rep := evidence.VerifyFile(path, keys)
	if !rep.OK || rep.Entries != 5 {
		t.Fatalf("log should verify with 5 entries: %+v", rep)
	}
	lf, _ := os.Open(path)
	defer lf.Close()
	entries, _ := evidence.ReadEntries(lf)
	var ev events.Event
	for _, e := range entries {
		if e.Kind == "event" {
			if err := e.DecodePayload(&ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	if ev.Type != events.Down || ev.Cause != "xid:79" || ev.TS != "2026-10-03T04:11:58Z" || ev.Evidence == "" {
		t.Fatalf("unexpected event %+v", ev)
	}
}

func TestGPUCountDrop(t *testing.T) {
	a, _, _ := setup(t)
	if err := a.HandleGPUCount(8, 8); err != nil {
		t.Fatal(err)
	}
	if err := a.HandleGPUCount(7, 8); err != nil {
		t.Fatal(err)
	}
	if st := a.Stats(); st.Events != 1 || st.Observations != 1 {
		t.Fatalf("unexpected stats %+v", st)
	}
	if CountGPUs("0, 00000000:18:00.0\n1, 00000000:2A:00.0\n\n") != 2 {
		t.Fatal("CountGPUs miscounted")
	}
}

func TestFollowFileReadsAppendedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kern.log")
	if err := os.WriteFile(path, []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := make(chan string, 4)
	go func() {
		_ = FollowFile(ctx, path, false, 20*time.Millisecond, func(s string) error { got <- s; return nil })
	}()
	time.Sleep(100 * time.Millisecond)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("new line\n")
	f.Close()
	select {
	case s := <-got:
		if s != "new line" {
			t.Fatalf("got %q, want the appended line only", s)
		}
	case <-ctx.Done():
		t.Fatal("appended line was not read")
	}
}
