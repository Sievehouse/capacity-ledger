// Package interval implements half-open intervals of Unix seconds and
// normalised interval sets. Every operation returns a normalised set, so the
// result never depends on the order of its inputs.
package interval

import "sort"

// Interval is the half-open range [Start, End) in Unix seconds.
type Interval struct {
	Start int64
	End   int64
}

// Len returns the length of the interval in seconds, or 0 if it is empty.
func (iv Interval) Len() int64 {
	if iv.End <= iv.Start {
		return 0
	}
	return iv.End - iv.Start
}

// Overlaps reports whether iv and o share at least one instant.
func (iv Interval) Overlaps(o Interval) bool {
	return iv.Start < o.End && o.Start < iv.End
}

// Set is a sorted slice of non-empty intervals that neither overlap nor touch.
type Set []Interval

// Normalize returns the set covering the same instants as in. Empty intervals
// are dropped; overlapping or adjacent intervals are merged.
func Normalize(in []Interval) Set {
	tmp := make([]Interval, 0, len(in))
	for _, iv := range in {
		if iv.End > iv.Start {
			tmp = append(tmp, iv)
		}
	}
	sort.Slice(tmp, func(i, j int) bool {
		if tmp[i].Start != tmp[j].Start {
			return tmp[i].Start < tmp[j].Start
		}
		return tmp[i].End < tmp[j].End
	})
	out := make(Set, 0, len(tmp))
	for _, iv := range tmp {
		n := len(out)
		if n > 0 && iv.Start <= out[n-1].End {
			if iv.End > out[n-1].End {
				out[n-1].End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}

// Total returns the number of seconds covered by the set.
func (s Set) Total() int64 {
	var t int64
	for _, iv := range s {
		t += iv.Len()
	}
	return t
}

// Union returns every instant in s or o.
func (s Set) Union(o Set) Set {
	all := make([]Interval, 0, len(s)+len(o))
	all = append(all, s...)
	all = append(all, o...)
	return Normalize(all)
}

// Intersect returns every instant in both s and o.
func (s Set) Intersect(o Set) Set {
	a, b := Normalize(s), Normalize(o)
	var out []Interval
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		lo := max(a[i].Start, b[j].Start)
		hi := min(a[i].End, b[j].End)
		if lo < hi {
			out = append(out, Interval{Start: lo, End: hi})
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return Normalize(out)
}

// Subtract returns every instant in s that is not in o.
func (s Set) Subtract(o Set) Set {
	a, b := Normalize(s), Normalize(o)
	var out []Interval
	j := 0
	for _, iv := range a {
		cur := iv.Start
		for j < len(b) && b[j].End <= cur {
			j++
		}
		for k := j; k < len(b) && b[k].Start < iv.End; k++ {
			if b[k].Start > cur {
				out = append(out, Interval{Start: cur, End: b[k].Start})
			}
			if b[k].End > cur {
				cur = b[k].End
			}
			if cur >= iv.End {
				break
			}
		}
		if cur < iv.End {
			out = append(out, Interval{Start: cur, End: iv.End})
		}
	}
	return Normalize(out)
}

// Clip returns the part of s inside [start, end).
func (s Set) Clip(start, end int64) Set {
	return s.Intersect(Set{{Start: start, End: end}})
}

// DropShorterThan removes intervals shorter than minLen seconds. Intervals of
// exactly minLen seconds are kept.
func (s Set) DropShorterThan(minLen int64) Set {
	n := Normalize(s)
	if minLen <= 0 {
		return n
	}
	out := make(Set, 0, len(n))
	for _, iv := range n {
		if iv.Len() >= minLen {
			out = append(out, iv)
		}
	}
	return out
}

// SplitAfter returns the first budget seconds of s, in time order, as head and
// everything after that as tail. A negative budget means no limit.
func (s Set) SplitAfter(budget int64) (head, tail Set) {
	n := Normalize(s)
	if budget < 0 {
		return n, Set{}
	}
	var h, t []Interval
	remaining := budget
	for _, iv := range n {
		switch {
		case remaining <= 0:
			t = append(t, iv)
		case iv.Len() <= remaining:
			h = append(h, iv)
			remaining -= iv.Len()
		default:
			cut := iv.Start + remaining
			h = append(h, Interval{Start: iv.Start, End: cut})
			t = append(t, Interval{Start: cut, End: iv.End})
			remaining = 0
		}
	}
	return Normalize(h), Normalize(t)
}

// AtLeast returns the instants covered by at least k of the given sets. Each
// set is normalised first, so one set counts at most once at any instant.
// It returns an empty set when k < 1.
func AtLeast(sets []Set, k int) Set {
	if k < 1 {
		return Set{}
	}
	type point struct {
		t int64
		d int
	}
	var pts []point
	for _, s := range sets {
		for _, iv := range Normalize(s) {
			pts = append(pts, point{iv.Start, 1}, point{iv.End, -1})
		}
	}
	// Ends sort before starts at the same instant: intervals are half-open.
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].t != pts[j].t {
			return pts[i].t < pts[j].t
		}
		return pts[i].d < pts[j].d
	})
	var out []Interval
	count := 0
	var start int64
	open := false
	for _, p := range pts {
		count += p.d
		if !open && count >= k {
			open = true
			start = p.t
		} else if open && count < k {
			open = false
			out = append(out, Interval{Start: start, End: p.t})
		}
	}
	return Normalize(out)
}
