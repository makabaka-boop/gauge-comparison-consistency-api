// Package solver maintains relative values of reference standards as a
// network of paired comparisons.
//
// Each comparison states: value[to] - value[from] = delta.
// Records are committed in UTF-8 byte order of their ids. The data
// structure is a weighted (potential-carrying) disjoint-set union backed
// by the spanning forest of every record that has been accepted; the
// forest is kept separately so a contradicting record can be traced
// against the state that existed just before it failed.
package solver

import "sort"

// Record is one comparison in the input.
type Record struct {
	ID    string
	From  string
	To    string
	Delta int64
}

// Step is one directed edge on the existing-forest path of a contradiction
// report, traversed from the failing record's From towards its To.
type Step struct {
	// From and To are standard ids in traversal order.
	From string `json:"from"`
	To   string `json:"to"`
	// RecordID is the accepted comparison that owns this forest edge.
	RecordID string `json:"recordId"`
	// SignedDelta is this edge's contribution to value[To]-value[From]
	// in traversal order, i.e. it equals the record's delta when the
	// edge is walked from its from endpoint and is negated otherwise.
	SignedDelta int64 `json:"signedDelta"`
	// Cumulative is value[current]-value[path start] after this step.
	Cumulative int64 `json:"cumulative"`
}

// Value is one standard's relative value inside a component.
type Value struct {
	StandardID string `json:"standardId"`
	Value      int64  `json:"value"`
}

// Component is one connected component expressed relative to its minimum
// standard id.
type Component struct {
	// Zero is the minimum standard id of the component and is fixed at 0.
	Zero string `json:"zero"`
	// Values are relative values ordered by standard id.
	Values []Value `json:"values"`
}

// Conflict is present exactly when the network is contradictory.
type Conflict struct {
	// RecordID is the id of the first record that failed consistency,
	// with processing order defined by UTF-8 byte order over record ids.
	RecordID string `json:"recordId"`
	From     string `json:"from"`
	To       string `json:"to"`
	// Delta is the failing record's claimed value[to]-value[from].
	Delta int64 `json:"delta"`
	// Expected is value[to]-value[from] derived from previously accepted
	// records only.
	Expected int64 `json:"expected"`
	// Residual is Delta - Expected and is necessarily non-zero.
	Residual int64 `json:"residual"`
	// Path walks the accepted-record forest From -> To. Closing it with
	// the failing record traversed backwards (To -> From, contribution
	// -Delta) forms the contradiction cycle.
	Path []Step `json:"path"`
	// Cycle is Path followed by the closing step that carries the
	// failing record backwards (To -> From, contribution -Delta); the
	// running total ends at Expected-Delta, which equals -Residual and is
	// necessarily non-zero.
	Cycle []Step `json:"cycle"`
}

// Result is the outcome of processing all comparisons.
type Result struct {
	// Consistent is true when no record contradicted previously accepted
	// records.
	Consistent bool `json:"consistent"`
	// Components is present for consistent networks, one entry per
	// connected component (isolated standards included), ordered by zero id.
	Components []Component `json:"components,omitempty"`
	// Conflict is present for contradictory networks.
	Conflict *Conflict `json:"conflict,omitempty"`
}

// Solve processes comparisons for the given standards.
//
// standards must be unique and records must reference them; the API layer
// validates those structural rules before calling Solve.
func Solve(standards []string, records []Record) Result {
	idx := make(map[string]int, len(standards))
	for i, id := range standards {
		idx[id] = i
	}

	// Committing order is fixed by UTF-8 byte order of record id.
	ordered := make([]Record, len(records))
	copy(ordered, records)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	n := len(standards)
	d := newDSU(n)

	// Spanning forest of accepted records. Each accepted merge adds one
	// edge; self comparisons add none.
	adj := make([][]adjEdge, n)

	for _, rec := range ordered {
		u := idx[rec.From]
		v := idx[rec.To]

		// Self comparison: value[x]-value[x] must be 0.
		if u == v {
			if rec.Delta != 0 {
				return buildConflict(rec, 0, nil, standards, adj, idx)
			}
			continue
		}

		ru, pu := d.find(u) // value[u]-value[ru] = pu
		rv, pv := d.find(v) // value[v]-value[rv] = pv

		if ru == rv {
			// Existing forest already fixes value[v]-value[u].
			expected := pv - pu
			if expected != rec.Delta {
				path := forestPath(adj, u, v, idx, standards)
				return buildConflict(rec, expected, path, standards, adj, idx)
			}
			// Redundant but consistent record: it is not part of the
			// spanning forest, so it cannot appear in a later trace.
			continue
		}

		// Merge by size, maintaining: value[x]-value[parent[x]] = pot[x].
		// Required: value[v]-value[u] = delta
		//        => value[rv]-value[ru] = pu + delta - pv.
		if d.size[ru] >= d.size[rv] {
			d.parent[rv] = ru
			d.pot[rv] = pu + rec.Delta - pv
			d.size[ru] += d.size[rv]
		} else {
			d.parent[ru] = rv
			d.pot[ru] = pv - rec.Delta - pu
			d.size[rv] += d.size[ru]
		}

		adj[u] = append(adj[u], adjEdge{
			to: v, recID: rec.ID, delta: rec.Delta, forward: true,
		})
		adj[v] = append(adj[v], adjEdge{
			to: u, recID: rec.ID, delta: rec.Delta, forward: false,
		})
	}

	return Result{Consistent: true, Components: components(d, standards)}
}

// buildConflict assembles the full report for the first failing record.
func buildConflict(rec Record, expected int64, path []Step, standards []string, adj [][]adjEdge, idx map[string]int) Result {
	if path == nil {
		// Self contradiction (from == to): the existing forest path is
		// empty and expected is 0.
		path = []Step{}
	}

	cycle := make([]Step, 0, len(path)+1)
	cycle = append(cycle, path...)

	var running int64
	if len(path) > 0 {
		running = path[len(path)-1].Cumulative
	}
	// Closing step walks the failing record backwards, To -> From,
	// contributing value[from]-value[to] = -delta.
	cycle = append(cycle, Step{
		From:        rec.To,
		To:          rec.From,
		RecordID:    rec.ID,
		SignedDelta: -rec.Delta,
		Cumulative:  running - rec.Delta,
	})

	return Result{
		Consistent: false,
		Conflict: &Conflict{
			RecordID: rec.ID,
			From:     rec.From,
			To:       rec.To,
			Delta:    rec.Delta,
			Expected: expected,
			Residual: rec.Delta - expected,
			Path:     path,
			Cycle:    cycle,
		},
	}
}

// adjEdge is an undirected forest edge. forward=true means the stored
// edge leaves from the owning record's From endpoint.
type adjEdge struct {
	to      int
	recID   string
	delta   int64
	forward bool
}

// forestPath reconstructs the unique accepted-forest path from u to v and
// evaluates it step by step.
func forestPath(adj [][]adjEdge, u, v int, idx map[string]int, standards []string) []Step {
	type via struct {
		prev int
		edge adjEdge
	}
	parent := make([]via, len(standards))
	seen := make([]bool, len(standards))
	queue := make([]int, 0, len(standards))
	queue = append(queue, u)
	seen[u] = true

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == v {
			break
		}
		for _, e := range adj[cur] {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			parent[e.to] = via{prev: cur, edge: e}
			queue = append(queue, e.to)
		}
	}

	// Walk v -> u through parent pointers.
	type rawStep struct {
		from, to int
		e        adjEdge
	}
	rev := make([]rawStep, 0)
	for cur := v; cur != u; cur = parent[cur].prev {
		rev = append(rev, rawStep{
			from: parent[cur].prev,
			to:   cur,
			e:    parent[cur].edge,
		})
	}

	// Reverse into traversal order u -> v.
	steps := make([]Step, 0, len(rev))
	var total int64
	for i := len(rev) - 1; i >= 0; i-- {
		rs := rev[i]
		// The edge stored at rs.from is the original record's From->To
		// exactly when forward is true.
		signed := rs.e.delta
		if !rs.e.forward {
			signed = -rs.e.delta
		}
		total += signed
		steps = append(steps, Step{
			From:        standards[rs.from],
			To:          standards[rs.to],
			RecordID:    rs.e.recID,
			SignedDelta: signed,
			Cumulative:  total,
		})
	}
	return steps
}

// components groups standards by DSU root and shifts each group so its
// minimum id has value zero. Groups and their values are ordered by id.
func components(d *dsu, standards []string) []Component {
	groups := make(map[int][]int)
	var roots []int
	for i := range standards {
		root, _ := d.find(i)
		if _, ok := groups[root]; !ok {
			roots = append(roots, root)
		}
		groups[root] = append(groups[root], i)
	}

	comps := make([]Component, 0, len(roots))
	for _, root := range roots {
		members := groups[root]
		sort.Slice(members, func(i, j int) bool {
			return standards[members[i]] < standards[members[j]]
		})

		zeroNode := members[0]
		_, pZero := d.find(zeroNode) // value[zero]-value[root]

		values := make([]Value, 0, len(members))
		for _, node := range members {
			_, pNode := d.find(node)
			values = append(values, Value{
				StandardID: standards[node],
				Value:      pNode - pZero,
			})
		}
		comps = append(comps, Component{Zero: standards[zeroNode], Values: values})
	}

	sort.Slice(comps, func(i, j int) bool {
		return comps[i].Zero < comps[j].Zero
	})
	return comps
}

// dsu is a weighted disjoint-set union. Invariant after find(x):
// value[x]-value[root] == pot[x].
type dsu struct {
	parent []int
	pot    []int64
	size   []int
}

func newDSU(n int) *dsu {
	d := &dsu{
		parent: make([]int, n),
		pot:    make([]int64, n),
		size:   make([]int, n),
	}
	for i := 0; i < n; i++ {
		d.parent[i] = i
		d.size[i] = 1
	}
	return d
}

// find returns the root and value[x]-value[root], compressing the path.
func (d *dsu) find(x int) (int, int64) {
	if d.parent[x] == x {
		return x, 0
	}

	// Gather the path to the root.
	path := []int{x}
	for d.parent[path[len(path)-1]] != path[len(path)-1] {
		path = append(path, d.parent[path[len(path)-1]])
	}
	root := path[len(path)-1]

	// potentials[node] is value[node]-value[next node on path].
	var sum int64
	for i := len(path) - 2; i >= 0; i-- {
		sum += d.pot[path[i]]
		d.parent[path[i]] = root
		d.pot[path[i]] = sum
	}
	return root, sum
}
