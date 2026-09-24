package solver

import (
	"sort"
)

// Solve 按 records 的 id UTF-8 字节序逐条传播约束。
//
// 调用方需保证 records 中引用的 from/to 均在 standards 内且结构合法；
// 本函数只负责数值约束的传播与诊断。
//
// 不变量（带势能并查集）：find(x) 后
//
//	pot[x] == value[x] - value[root(x)]
//
// 因此任意两同根节点满足 value[b] - value[a] == pot[b] - pot[a]。
//
// 只有把两个分量真正连起来的记录才会进入森林 forest；已经同根的记录
// 只做校验。这样森林中 from→to 的简单路径唯一，诊断给出的矛盾环也确定。
func Solve(standards []string, records []Record) Result {
	index := make(map[string]int, len(standards))
	for i, s := range standards {
		index[s] = i
	}

	// 按记录 id 的 UTF-8 字节序处理；复制一份，不改动调用方切片。
	ordered := make([]Record, len(records))
	copy(ordered, records)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].ID < ordered[j].ID
	})

	n := len(standards)
	parent := make([]int, n)
	size := make([]int, n)
	// pot[x] = value[x] - value[parent(x)]；根的 pot 恒为 0。
	pot := make([]int64, n)
	for i := range parent {
		parent[i] = i
		size[i] = 1
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] == x {
			return x
		}
		p := parent[x]
		root := find(p)
		pot[x] += pot[p] // 路径压缩后 pot[x] 直连根：value[x] - value[root]
		parent[x] = root
		return root
	}

	// forest 仅由“连接两个不同分量”的已接受记录构成，因此始终是森林。
	adj := make([][]adjEdge, n)

	for ri := range ordered {
		rec := ordered[ri]
		a, b := index[rec.From], index[rec.To]

		// 自比较：value[x] - value[x] 必须为 0，否则当场矛盾。
		if a == b {
			if rec.Delta != 0 {
				return Result{Consistent: false, Conflict: selfConflict(rec)}
			}
			continue
		}

		ra, rb := find(a), find(b)
		if ra == rb {
			// 同分量：隐含差值 value[b]-value[a] 为 pot[b] - pot[a]。
			implied := pot[b] - pot[a]
			if implied != rec.Delta {
				path := forestPath(a, b, adj, ordered, standards)
				return Result{
					Consistent: false,
					Conflict: &Conflict{
						Record:       rec,
						ImpliedDelta: implied,
						Mismatch:     rec.Delta - implied,
						Path:         path,
						Loop: Loop{
							Nodes:       loopNodes(path),
							PathTotal:   path.Total,
							RecordDelta: rec.Delta,
							Sum:         path.Total - rec.Delta,
						},
					},
				}
			}
			continue
		}

		// 合并两个分量并维护势能。
		link(a, b, ra, rb, rec.Delta, parent, size, pot)

		// 不论 ra/rb 谁挂到谁下，边在无向森林中都是同一条。
		adj[a] = append(adj[a], adjEdge{to: b, ri: ri})
		adj[b] = append(adj[b], adjEdge{to: a, ri: ri})
	}

	// 全部一致：以每个连通分量中 id 最小的标准件为零点输出相对值。
	rootOf := make([]int, n)
	rootMin := map[int]int{} // 各分量（以根为键）最小标准件的节点下标
	for i := range standards {
		r := find(i)
		rootOf[i] = r
		if cur, ok := rootMin[r]; !ok || standards[i] < standards[cur] {
			rootMin[r] = i
		}
	}

	values := make(Int64Map, n)
	for i, s := range standards {
		z := rootMin[rootOf[i]]
		// 同根：pot[i] = value[i]-value[root]，pot[z] 同理，
		// 相对零点值 value[i]-value[z] = pot[i] - pot[z]。
		values[s] = pot[i] - pot[z]
	}

	return Result{Consistent: true, Values: values}
}

// link 按大小合并两个分量并维护势能。约束：value[b] - value[a] = delta。
func link(a, b, ra, rb int, delta int64, parent, size []int, pot []int64) {
	// find 之后 pot[a] = value[a] - value[ra]，pot[b] = value[b] - value[rb]。
	if size[ra] >= size[rb] {
		// rb 挂到 ra 下，令 pot[rb] = value[rb] - value[ra]
		// = (value[b] - pot[b]) - (value[a] - pot[a])
		// = delta + pot[a] - pot[b]。
		parent[rb] = ra
		pot[rb] = delta + pot[a] - pot[b]
		size[ra] += size[rb]
	} else {
		// ra 挂到 rb 下，令 pot[ra] = value[ra] - value[rb]
		// = pot[b] - pot[a] - delta。
		parent[ra] = rb
		pot[ra] = pot[b] - pot[a] - delta
		size[rb] += size[ra]
	}
}

type adjEdge struct {
	to int // 对端节点下标
	ri int // 对应记录在 ordered 中的下标
}

// forestPath 在仅含已接受记录的无向森林中，用 BFS 求 a→b 的唯一简单路径，
// 并按行走方向还原每一步的符号与累计差值，使其可直接与失效记录对照。
func forestPath(a, b int, adj [][]adjEdge, ordered []Record, standards []string) Path {
	n := len(adj)
	preNode := make([]int, n)
	preEdge := make([]int, n)
	visited := make([]bool, n)
	for i := range preNode {
		preNode[i] = -1
		preEdge[i] = -1
	}
	visited[a] = true

	queue := []int{a}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		if u == b {
			break
		}
		for _, e := range adj[u] {
			if !visited[e.to] {
				visited[e.to] = true
				preNode[e.to] = u
				preEdge[e.to] = e.ri
				queue = append(queue, e.to)
			}
		}
	}

	// 从 b 沿前驱回溯到 a，再反转为从 a 出发的顺序。
	var backNodes []int
	var backEdges []int
	for u := b; u != a; u = preNode[u] {
		backNodes = append(backNodes, u)
		backEdges = append(backEdges, preEdge[u])
	}
	backNodes = append(backNodes, a)

	steps := make([]Step, 0, len(backEdges))
	running := make([]int64, 0, len(backEdges))
	var total int64

	// backNodes 形如 [b, ..., a]；第 k 跳（回溯序）端点为
	// backNodes[k] -> backNodes[k+1]，所用记录为 ordered[backEdges[k]]。
	for k := len(backEdges) - 1; k >= 0; k-- {
		cur := backNodes[k+1]
		nxt := backNodes[k]
		rec := ordered[backEdges[k]]

		var signed int64
		var direction string
		if indexOf(rec, standards, cur, nxt) {
			// 行走方向与记录方向一致：value[nxt]-value[cur] = delta。
			direction = "forward"
			signed = rec.Delta
		} else {
			// 反向行走：value[nxt]-value[cur] = -delta。
			direction = "reverse"
			signed = -rec.Delta
		}

		total += signed
		steps = append(steps, Step{
			RecordID:    rec.ID,
			From:        standards[cur],
			To:          standards[nxt],
			Direction:   direction,
			SignedDelta: signed,
		})
		running = append(running, total)
	}

	return Path{
		From:         standards[a],
		To:           standards[b],
		Steps:        steps,
		RunningTotal: running,
		Total:        total,
	}
}

// indexOf 判断给定记录的方向是否为 cur -> nxt。
func indexOf(rec Record, standards []string, cur, nxt int) bool {
	return rec.From == standards[cur] && rec.To == standards[nxt]
}

// loopNodes 按顺序给出闭合环的节点序列：先走森林路径 a→…→b，
// 再由失效记录 b→a 闭合。
func loopNodes(p Path) []string {
	nodes := make([]string, 0, len(p.Steps)+2)
	nodes = append(nodes, p.From)
	for _, s := range p.Steps {
		nodes = append(nodes, s.To)
	}
	nodes = append(nodes, p.From)
	return nodes
}

// selfConflict 构造自比较矛盾（value[x]-value[x] 非 0）的诊断：
// 此前没有路径，环仅由该失效记录自身闭合。
func selfConflict(rec Record) *Conflict {
	return &Conflict{
		Record:       rec,
		ImpliedDelta: 0,
		Mismatch:     rec.Delta,
		Path: Path{
			From:         rec.From,
			To:           rec.To,
			Steps:        []Step{},
			RunningTotal: []int64{},
			Total:        0,
		},
		Loop: Loop{
			Nodes:       []string{rec.From, rec.From},
			PathTotal:   0,
			RecordDelta: rec.Delta,
			Sum:         -rec.Delta,
		},
	}
}
