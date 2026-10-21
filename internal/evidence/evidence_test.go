package evidence

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newWriter(t *testing.T) (*Writer, string, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	w, err := OpenWriter(path, priv)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 10, 3, 4, 12, 0, 0, time.UTC)
	w.SetClock(func() time.Time { clock = clock.Add(time.Second); return clock })
	return w, path, pub
}

func appendSome(t *testing.T, w *Writer) {
	t.Helper()
	for i, kind := range []string{"observation", "event", "statement"} {
		if _, err := w.Append(kind, map[string]any{"i": i, "node": "gpu-003"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyAcceptsIntactLog(t *testing.T) {
	w, path, pub := newWriter(t)
	appendSome(t, w)
	rep := VerifyFile(path, map[string]ed25519.PublicKey{KeyID(pub): pub})
	if !rep.OK || rep.Entries != 3 || rep.MerkleRoot == "" {
		t.Fatalf("expected a clean report, got %+v", rep)
	}
}

func TestReopenContinuesChain(t *testing.T) {
	w, path, pub := newWriter(t)
	appendSome(t, w)
	w2, err := OpenWriter(path, w.priv)
	if err != nil {
		t.Fatal(err)
	}
	if w2.NextSeq() != 4 {
		t.Fatalf("next seq %d, want 4", w2.NextSeq())
	}
	if _, err := w2.Append("event", map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if rep := VerifyFile(path, map[string]ed25519.PublicKey{KeyID(pub): pub}); !rep.OK || rep.Entries != 4 {
		t.Fatalf("reopened chain should verify, got %+v", rep)
	}
}

func TestDetectsTamperingDeletionAndWrongKey(t *testing.T) {
	w, path, pub := newWriter(t)
	appendSome(t, w)
	keys := map[string]ed25519.PublicKey{KeyID(pub): pub}
	orig, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(orig)), "\n")

	edited := strings.Replace(string(orig), `"node":"gpu-003"`, `"node":"gpu-004"`, 1)
	if rep := Verify(strings.NewReader(edited), keys); rep.OK {
		t.Fatal("edited payload was not detected")
	}
	deleted := lines[0] + "\n" + lines[2] + "\n"
	if rep := Verify(strings.NewReader(deleted), keys); rep.OK {
		t.Fatal("deleted entry was not detected")
	}
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if rep := Verify(bytes.NewReader(orig), map[string]ed25519.PublicKey{KeyID(otherPub): otherPub}); rep.OK {
		t.Fatal("unknown signing key was not detected")
	}
}

func TestMerkleRootStructure(t *testing.T) {
	a, b, c := []byte("a"), []byte("b"), []byte("c")
	if !bytes.Equal(MerkleRoot([][]byte{a}), leafHash(a)) {
		t.Fatal("single-leaf root must be the leaf hash")
	}
	want := nodeHash(nodeHash(leafHash(a), leafHash(b)), leafHash(c))
	if !bytes.Equal(MerkleRoot([][]byte{a, b, c}), want) {
		t.Fatal("three-leaf root must split at the largest power of two below n")
	}
	if bytes.Equal(MerkleRoot([][]byte{a, b}), MerkleRoot([][]byte{b, a})) {
		t.Fatal("root must depend on order")
	}
}

func TestKeyFilesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	privPath, pubPath, id, err := GenerateKeyPair(dir)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := LoadPrivateKey(privPath)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := LoadPublicKey(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	if KeyID(pub) != id || !pub.Equal(priv.Public()) {
		t.Fatal("key files do not round-trip")
	}
	if _, _, _, err := GenerateKeyPair(dir); err == nil {
		t.Fatal("generating into a directory with keys must fail")
	}
}
