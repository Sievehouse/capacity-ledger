package interval

import (
	"math/rand"
	"reflect"
	"testing"
)

func iv(a, b int64) Interval { return Interval{Start: a, End: b} }

func TestNormalizeMergesOverlapAndAdjacency(t *testing.T) {
	got := Normalize([]Interval{iv(10, 20), iv(0, 5), iv(5, 8), iv(15, 30), iv(40, 40)})
	want := Set{iv(0, 8), iv(10, 30)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSubtract(t *testing.T) {
	s := Set{iv(0, 100)}
	got := s.Subtract(Set{iv(10, 20), iv(50, 60), iv(90, 120)})
	want := Set{iv(0, 10), iv(20, 50), iv(60, 90)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := (Set{iv(0, 10)}).Subtract(Set{iv(-5, 15)}); len(got) != 0 {
		t.Fatalf("full cover should leave nothing, got %v", got)
	}
	// A subtrahend spanning two intervals must cut both.
	got = Set{iv(0, 10), iv(20, 30)}.Subtract(Set{iv(5, 25)})
	want = Set{iv(0, 5), iv(25, 30)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestIntersectAndClip(t *testing.T) {
	got := Set{iv(0, 10), iv(20, 30)}.Intersect(Set{iv(5, 25)})
	want := Set{iv(5, 10), iv(20, 25)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := (Set{iv(-10, 10)}).Clip(0, 5); !reflect.DeepEqual(got, Set{iv(0, 5)}) {
		t.Fatalf("clip got %v", got)
	}
}

func TestDropShorterThanKeepsBoundary(t *testing.T) {
	got := Set{iv(0, 299), iv(1000, 1300)}.DropShorterThan(300)
	if !reflect.DeepEqual(got, Set{iv(1000, 1300)}) {
		t.Fatalf("got %v", got)
	}
}

func TestSplitAfter(t *testing.T) {
	head, tail := Set{iv(0, 10), iv(20, 30)}.SplitAfter(15)
	if !reflect.DeepEqual(head, Set{iv(0, 10), iv(20, 25)}) || !reflect.DeepEqual(tail, Set{iv(25, 30)}) {
		t.Fatalf("head %v tail %v", head, tail)
	}
	head, tail = Set{iv(0, 10)}.SplitAfter(-1)
	if head.Total() != 10 || len(tail) != 0 {
		t.Fatalf("unlimited budget: head %v tail %v", head, tail)
	}
}

func TestAtLeastHandlesHandOver(t *testing.T) {
	// A ends exactly when B starts; with C always down, two nodes are down
	// throughout, so the k=2 set is one continuous interval.
	a, b, c := Set{iv(0, 10)}, Set{iv(10, 20)}, Set{iv(0, 20)}
	got := AtLeast([]Set{a, b, c}, 2)
	if !reflect.DeepEqual(got, Set{iv(0, 20)}) {
		t.Fatalf("got %v", got)
	}
	got = AtLeast([]Set{{iv(0, 120)}, {iv(30, 105)}, {iv(15, 75)}}, 3)
	if !reflect.DeepEqual(got, Set{iv(30, 75)}) {
		t.Fatalf("got %v", got)
	}
	if got := AtLeast([]Set{a}, 0); len(got) != 0 {
		t.Fatalf("k=0 should be empty, got %v", got)
	}
}

// Brute-force cross-check of the set algebra on small random inputs.
func TestAlgebraAgainstBruteForce(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	randomSet := func() Set {
		var ivs []Interval
		for i := 0; i < r.Intn(5); i++ {
			a := int64(r.Intn(60))
			ivs = append(ivs, iv(a, a+int64(r.Intn(15))))
		}
		return Normalize(ivs)
	}
	covered := func(s Set, x int64) bool {
		for _, v := range s {
			if x >= v.Start && x < v.End {
				return true
			}
		}
		return false
	}
	for trial := 0; trial < 500; trial++ {
		a, b := randomSet(), randomSet()
		u, in, d := a.Union(b), a.Intersect(b), a.Subtract(b)
		for x := int64(-1); x < 80; x++ {
			ca, cb := covered(a, x), covered(b, x)
			if covered(u, x) != (ca || cb) || covered(in, x) != (ca && cb) || covered(d, x) != (ca && !cb) {
				t.Fatalf("trial %d x=%d a=%v b=%v", trial, x, a, b)
			}
		}
	}
}
