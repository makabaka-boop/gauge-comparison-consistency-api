package solver

import (
	"fmt"
	"math/rand"
	"testing"
)

// verifyValues checks that a consistent result reports, for every
// component, values equal to potentials shifted so the minimum id is zero,
// and that ids are in ascending order.
func verifyValues(t *testing.T, standards []string, pot map[string]int64, res Result) {
	t.Helper()
	if !res.Consistent {
		t.Fatalf("expected consistent result, got conflict: %+v", res.Conflict)
	}
	byID := make(map[string]int64)
	for _, comp := range res.Components {
		var prev string
		for i, v := range comp.Values {
			if i > 0 && !(prev < v.StandardID) {
				t.Fatalf("values not ordered by id: %q before %q", prev, v.StandardID)
			}
			prev = v.StandardID
			byID[v.StandardID] = v.Value
		}
	}
	// Components group standards by connectivity in the test graphs;
	// tests below generate connected networks, so one component is
	// expected and all potentials must match up to the zero shift.
	if len(res.Components) != 1 {
		t.Fatalf("expected 1 component, got %d", len(res.Components))
	}
	zero := res.Components[0].Zero
	if zero != minID(standards) {
		t.Fatalf("zero = %q, want minimum id %q", zero, minID(standards))
	}
	if byID[zero] != 0 {
		t.Fatalf("zero point value = %d, want 0", byID[zero])
	}
	for _, id := range standards {
		want := pot[id] - pot[zero]
		if byID[id] != want {
			t.Fatalf("value[%s] = %d, want %d", id, byID[id], want)
		}
	}
}

func minID(ids []string) string {
	m := ids[0]
	for _, id := range ids[1:] {
		if id < m {
			m = id
		}
	}
	return m
}

// verifyPathChains checks a reported path is a contiguous walk from->to
// and returns the running total of its signed deltas.
func verifyPathChains(t *testing.T, path []Step, from, to string) int64 {
	t.Helper()
	if len(path) == 0 {
		if from != to {
			t.Fatalf("empty path but %q != %q", from, to)
		}
		return 0
	}
	if path[0].From != from {
		t.Fatalf("path starts at %q, want %q", path[0].From, from)
	}
	if path[len(path)-1].To != to {
		t.Fatalf("path ends at %q, want %q", path[len(path)-1].To, to)
	}
	var total int64
	for i, s := range path {
		if i > 0 && path[i-1].To != s.From {
			t.Fatalf("path not contiguous between step %d and %d", i-1, i)
		}
		total += s.SignedDelta
		if s.Cumulative != total {
			t.Fatalf("step %d cumulative = %d, want running total %d", i, s.Cumulative, total)
		}
	}
	return total
}

func TestBasicTriangleConsistent(t *testing.T) {
	// a=0, b=2 (b-a=2), c=5 (c-a=5); c-b=3 closes the triangle.
	standards := []string{"a", "b", "c"}
	records := []Record{
		{ID: "e1", From: "a", To: "b", Delta: 2},
		{ID: "e2", From: "a", To: "c", Delta: 5},
		{ID: "e3", From: "b", To: "c", Delta: 3},
	}
	res := Solve(standards, records)
	verifyValues(t, standards, map[string]int64{"a": 0, "b": 2, "c": 5}, res)
}

func TestZeroPointIsMinimumID(t *testing.T) {
	// Input order is arbitrary; the minimum id, not the first id, is zero.
	standards := []string{"zzz", "mmm", "aaa"}
	records := []Record{
		{ID: "r1", From: "zzz", To: "mmm", Delta: -7},
		{ID: "r2", From: "mmm", To: "aaa", Delta: -3},
	}
	res := Solve(standards, records)
	// zzz-mmm= -7 => mmm = zzz-7; aaa-mmm = -3 => aaa = zzz-10.
	// Relative to aaa=0: mmm=3, zzz=10.
	verifyValues(t, standards, map[string]int64{"aaa": 0, "mmm": 3, "zzz": 10}, res)
}

func TestReverseEdgesAreEquivalent(t *testing.T) {
	// Stating to->from with -delta must mean the same constraint.
	standards := []string{"n1", "n2", "n3", "n4"}
	records := []Record{
		{ID: "e1", From: "n2", To: "n1", Delta: -4}, // n1-n2 = -4
		{ID: "e2", From: "n3", To: "n2", Delta: -9}, // n2-n3 = -9
		{ID: "e3", From: "n4", To: "n3", Delta: -2}, // n3-n4 = -2
		{ID: "e4", From: "n1", To: "n4", Delta: 15}, // closes: n4-n1 = 15
	}
	res := Solve(standards, records)
	// With n2=0: n1=-4, n3=9, n4=11. Minimum id is n1, so shift by +4.
	verifyValues(t, standards, map[string]int64{"n1": 0, "n2": 4, "n3": 13, "n4": 15}, res)
}

func TestParallelComparisons(t *testing.T) {
	standards := []string{"a", "b"}
	records := []Record{
		{ID: "p1", From: "a", To: "b", Delta: 6},
		{ID: "p2", From: "b", To: "a", Delta: -6}, // same constraint reversed
		{ID: "p3", From: "a", To: "b", Delta: 6},  // redundant duplicate
	}
	res := Solve(standards, records)
	verifyValues(t, standards, map[string]int64{"a": 0, "b": 6}, res)

	// A fourth parallel record with a flipped sign must fail, and the
	// path is the single accepted edge p1.
	records = append(records, Record{ID: "p4", From: "a", To: "b", Delta: -6})
	res = Solve(standards, records)
	if res.Consistent || res.Conflict == nil {
		t.Fatalf("expected conflict on p4, got %+v", res)
	}
	c := res.Conflict
	if c.RecordID != "p4" || c.Expected != 6 || c.Delta != -6 || c.Residual != -12 {
		t.Fatalf("unexpected conflict: %+v", c)
	}
	total := verifyPathChains(t, c.Path, "a", "b")
	if total != c.Expected {
		t.Fatalf("path total %d != expected %d", total, c.Expected)
	}
	if len(c.Path) != 1 || c.Path[0].RecordID != "p1" || c.Path[0].SignedDelta != 6 {
		t.Fatalf("path should reuse p1, got %+v", c.Path)
	}
	// Only the spanning edge p1 may appear in the explanatory path;
	// redundant accepted records must not.
	for _, s := range c.Path {
		if s.RecordID == "p4" {
			t.Fatalf("failing record must not appear inside its own path")
		}
	}
}

func TestSelfComparisons(t *testing.T) {
	standards := []string{"a", "b"}

	// delta 0 on a self comparison is trivially consistent.
	res := Solve(standards, []Record{
		{ID: "s0", From: "a", To: "a", Delta: 0},
		{ID: "e1", From: "a", To: "b", Delta: 1},
		{ID: "s1", From: "b", To: "b", Delta: 0},
	})
	verifyValues(t, standards, map[string]int64{"a": 0, "b": 1}, res)

	// Nonzero self comparison contradicts value[x]-value[x]=0; there is
	// no forest path, and the cycle consists of the record alone.
	res = Solve(standards, []Record{
		{ID: "sbad", From: "a", To: "a", Delta: 3},
	})
	if res.Consistent {
		t.Fatalf("expected self-comparison conflict")
	}
	c := res.Conflict
	if c.RecordID != "sbad" || c.Expected != 0 || c.Delta != 3 || c.Residual != 3 {
		t.Fatalf("unexpected self conflict: %+v", c)
	}
	if len(c.Path) != 0 {
		t.Fatalf("self path must be empty, got %+v", c.Path)
	}
	if len(c.Cycle) != 1 || c.Cycle[0].RecordID != "sbad" ||
		c.Cycle[0].From != "a" || c.Cycle[0].To != "a" ||
		c.Cycle[0].SignedDelta != -3 || c.Cycle[0].Cumulative != -3 {
		t.Fatalf("self cycle must be the single backwards record, got %+v", c.Cycle)
	}
}

func TestProcessingOrderIsRecordIDByteOrder(t *testing.T) {
	// Triangle where either e2 or e3 could be "the failing edge"
	// depending on order; id byte order decides, never input order.
	standards := []string{"a", "b", "c"}
	good := []Record{
		{ID: "e1", From: "a", To: "b", Delta: 2},
		{ID: "e2", From: "b", To: "c", Delta: 3},
	}
	// c-a via e1+e2 is 5; claim 9 instead.
	bad := Record{ID: "e3", From: "a", To: "c", Delta: 9}

	for _, order := range [][]Record{
		{bad, good[1], good[0]},
		{good[1], bad, good[0]},
		{good[0], bad, good[1]},
		{good[1], good[0], bad},
	} {
		res := Solve(standards, order)
		if res.Consistent || res.Conflict == nil || res.Conflict.RecordID != "e3" {
			t.Fatalf("input order %v: expected e3 conflict, got %+v", order, res.Conflict)
		}
		if res.Conflict.Expected != 5 || res.Conflict.Residual != 4 {
			t.Fatalf("input order %v: unexpected conflict %+v", order, res.Conflict)
		}
		total := verifyPathChains(t, res.Conflict.Path, "a", "c")
		if total != 5 {
			t.Fatalf("input order %v: path total = %d, want 5", order, total)
		}
	}
}

func TestLongChainConflict(t *testing.T) {
	// A chain of 20 standards: local triangle checks never see the long
	// loop; the solver must still trace the full accepted path.
	const n = 20
	standards := make([]string, n)
	records := make([]Record, 0, n)
	pot := map[string]int64{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("s%02d", i)
		standards[i] = id
		pot[id] = int64(i * 7)
		if i > 0 {
			records = append(records, Record{
				ID:    fmt.Sprintf("chain%02d", i),
				From:  fmt.Sprintf("s%02d", i-1),
				To:    id,
				Delta: 7,
			})
		}
	}
	// Close the long loop with a sign error: s19-s0 should be 19*7=133.
	bad := Record{ID: "zzz-close", From: "s00", To: "s19", Delta: -133}
	res := Solve(standards, append(records, bad))
	if res.Consistent {
		t.Fatalf("expected long-loop conflict")
	}
	c := res.Conflict
	if c.RecordID != "zzz-close" || c.Expected != 133 || c.Delta != -133 {
		t.Fatalf("unexpected conflict: %+v", c)
	}
	if len(c.Path) != n-1 {
		t.Fatalf("path length = %d, want %d", len(c.Path), n-1)
	}
	total := verifyPathChains(t, c.Path, "s00", "s19")
	if total != 133 {
		t.Fatalf("long path total = %d, want 133", total)
	}
	verifyCycle(t, c)
}

// verifyCycle checks the reported cycle is a closed walk whose signed
// total is the non-zero loop closure error Expected-Delta = -Residual.
func verifyCycle(t *testing.T, c *Conflict) {
	t.Helper()
	if c.Residual == 0 {
		t.Fatal("residual must be non-zero")
	}
	cyc := c.Cycle
	if len(cyc) < 1 {
		t.Fatal("cycle must contain at least the failing record")
	}
	if cyc[0].From != c.From {
		t.Fatalf("cycle starts at %q, want %q", cyc[0].From, c.From)
	}
	if cyc[len(cyc)-1].To != c.From {
		t.Fatalf("cycle does not close: ends at %q, want %q", cyc[len(cyc)-1].To, c.From)
	}
	var total int64
	for i, s := range cyc {
		if i > 0 && cyc[i-1].To != s.From {
			t.Fatalf("cycle not contiguous at step %d", i)
		}
		total += s.SignedDelta
		if s.Cumulative != total {
			t.Fatalf("cycle cumulative at step %d = %d, want %d", i, s.Cumulative, total)
		}
	}
	if total != -c.Residual {
		t.Fatalf("cycle total = %d, want expected-delta = %d", total, -c.Residual)
	}
	// The failing record appears exactly once, as the closing step.
	var closes int
	for _, s := range cyc {
		if s.RecordID == c.RecordID {
			closes++
		}
	}
	if closes != 1 || cyc[len(cyc)-1].RecordID != c.RecordID {
		t.Fatalf("failing record must only appear as the closing cycle step")
	}
}

// randomConsistentNetwork builds a connected graph whose records are all
// consistent with the hidden potentials. Tree records get ids sorting
// before redundant records so that a flipped redundant record is always
// detected at itself rather than at a later tree edge.
func randomConsistentNetwork(t *testing.T, rng *rand.Rand) ([]string, map[string]int64, []Record) {
	t.Helper()
	n := 2 + rng.Intn(39) // 2..40 standards
	standards := make([]string, n)
	for i := range standards {
		standards[i] = fmt.Sprintf("std%03d", i)
	}

	pot := make(map[string]int64, n)
	for _, id := range standards {
		pot[id] = rng.Int63n(2_000_001) - 1_000_000
	}

	orient := func(a, b string) (from, to string, delta int64) {
		if rng.Intn(2) == 0 {
			return a, b, pot[b] - pot[a]
		}
		return b, a, pot[a] - pot[b]
	}

	var records []Record

	// Random spanning tree via shuffled node attachment.
	order := rng.Perm(n)
	for i := 1; i < n; i++ {
		a := standards[order[i]]
		b := standards[order[rng.Intn(i)]]
		from, to, delta := orient(a, b)
		records = append(records, Record{
			ID: fmt.Sprintf("aa-tree%04d", i), From: from, To: to, Delta: delta,
		})
	}

	// Random redundant consistent records, including parallels and selves.
	// At least three are generated so conflict injection always has a
	// non-self, nonzero candidate.
	extra := 3 + rng.Intn(3*n)
	for i := 0; i < extra; i++ {
		a, b := standards[rng.Intn(n)], standards[rng.Intn(n)]
		if i < 2 {
			// Guarantee at least two eligible non-self candidates for
			// conflict injection.
			for b == a {
				b = standards[rng.Intn(n)]
			}
		}
		from, to, delta := orient(a, b)
		records = append(records, Record{
			ID: fmt.Sprintf("zz-red%05d", i), From: from, To: to, Delta: delta,
		})
	}
	return standards, pot, records
}

func TestRandomConsistentNetworks(t *testing.T) {
	rng := rand.New(rand.NewSource(20260923))
	for iter := 0; iter < 200; iter++ {
		standards, pot, records := randomConsistentNetwork(t, rng)

		// Shuffle input order; byte order of ids must still govern.
		shuffled := append([]Record(nil), records...)
		rng.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		res := Solve(standards, shuffled)
		verifyValues(t, standards, pot, res)

		// Determinism: solving again must be identical.
		res2 := Solve(standards, shuffled)
		if fmt.Sprintf("%v", res) != fmt.Sprintf("%v", res2) {
			t.Fatalf("non-deterministic result on iteration %d", iter)
		}
	}
}

func TestRandomInjectedSingleConflict(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	for iter := 0; iter < 200; iter++ {
		standards, pot, records := randomConsistentNetwork(t, rng)

		// Choose a redundant, non-self, nonzero record and flip its sign.
		var eligible []int
		for i, r := range records {
			if r.ID >= "zz" && r.From != r.To && r.Delta != 0 {
				eligible = append(eligible, i)
			}
		}
		if len(eligible) == 0 {
			iter--
			continue
		}
		idx := eligible[rng.Intn(len(eligible))]
		injected := records[idx]
		injected.Delta = -injected.Delta

		input := append([]Record(nil), records...)
		rng.Shuffle(len(input), func(i, j int) {
			input[i], input[j] = input[j], input[i]
		})
		// Replace the chosen logical record regardless of shuffle.
		for i := range input {
			if input[i].ID == injected.ID {
				input[i] = injected
			}
		}

		res := Solve(standards, input)
		if res.Consistent || res.Conflict == nil {
			t.Fatalf("iter %d: injected conflict not detected", iter)
		}
		c := res.Conflict
		if c.RecordID != injected.ID {
			t.Fatalf("iter %d: failing record = %q, want injected %q",
				iter, c.RecordID, injected.ID)
		}
		if c.From != injected.From || c.To != injected.To || c.Delta != injected.Delta {
			t.Fatalf("iter %d: conflict header mismatch: %+v", iter, c)
		}

		wantExpected := pot[injected.To] - pot[injected.From]
		if c.Expected != wantExpected {
			t.Fatalf("iter %d: expected = %d, want %d", iter, c.Expected, wantExpected)
		}
		if c.Residual != injected.Delta-wantExpected || c.Residual == 0 {
			t.Fatalf("iter %d: bad residual %d", iter, c.Residual)
		}

		// Path signs must agree with the original (un-flipped) records.
		original := map[string]Record{}
		for _, r := range records {
			original[r.ID] = r
		}
		var running int64
		for _, s := range c.Path {
			r, ok := original[s.RecordID]
			if !ok || s.RecordID == injected.ID {
				t.Fatalf("iter %d: path uses non-accepted record %q", iter, s.RecordID)
			}
			var wantSigned int64
			if r.From == s.From && r.To == s.To {
				wantSigned = r.Delta
			} else if r.From == s.To && r.To == s.From {
				wantSigned = -r.Delta
			} else {
				t.Fatalf("iter %d: step %+v does not match record %+v", iter, s, r)
			}
			if s.SignedDelta != wantSigned {
				t.Fatalf("iter %d: wrong sign on step %+v", iter, s)
			}
			running += wantSigned
			if s.Cumulative != running {
				t.Fatalf("iter %d: cumulative mismatch", iter)
			}
		}

		total := verifyPathChains(t, c.Path, c.From, c.To)
		if total != wantExpected {
			t.Fatalf("iter %d: path total = %d, want %d", iter, total, wantExpected)
		}
		verifyCycle(t, c)
	}
}

func TestDisconnectedComponents(t *testing.T) {
	standards := []string{"a", "b", "c", "d"}
	records := []Record{
		{ID: "r1", From: "a", To: "b", Delta: 4},
		{ID: "r2", From: "c", To: "d", Delta: -2},
	}
	res := Solve(standards, records)
	if !res.Consistent {
		t.Fatalf("unexpected conflict: %+v", res.Conflict)
	}
	if len(res.Components) != 2 {
		t.Fatalf("want 2 components, got %d", len(res.Components))
	}
	if res.Components[0].Zero != "a" || res.Components[1].Zero != "c" {
		t.Fatalf("component zeros = %q, %q", res.Components[0].Zero, res.Components[1].Zero)
	}
}

func TestLargeDeltasNoOverflow(t *testing.T) {
	standards := []string{"a", "b", "c"}
	records := []Record{
		{ID: "r1", From: "a", To: "b", Delta: 1_000_000_000},
		{ID: "r2", From: "b", To: "c", Delta: 1_000_000_000},
		{ID: "r3", From: "a", To: "c", Delta: 2_000_000_000}, // internally consistent
	}
	res := Solve(standards, records)
	verifyValues(t, standards, map[string]int64{"a": 0, "b": 1e9, "c": 2e9}, res)
}
