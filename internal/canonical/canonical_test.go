package canonical

import "testing"

func TestKeyOrderDoesNotMatter(t *testing.T) {
	a := map[string]any{"b": 1, "a": "x", "c": []any{3, 2}}
	b := map[string]any{"c": []any{3, 2}, "a": "x", "b": 1}
	ha, err := SHA256Hex(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := SHA256Hex(b)
	if ha != hb {
		t.Fatalf("hashes differ: %s vs %s", ha, hb)
	}
}

func TestStructAndMapAgree(t *testing.T) {
	type s struct {
		Z string `json:"z"`
		A int    `json:"a"`
	}
	got, err := Marshal(s{Z: "<&>", A: 7})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":7,"z":"<&>"}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNumbersKeptExactly(t *testing.T) {
	got, err := Marshal(map[string]any{"n": 12345678901234})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"n":12345678901234}` {
		t.Fatalf("got %s", got)
	}
}
