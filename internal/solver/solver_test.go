package solver

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// 一致网络（三角形，全部用 record id 的字节序处理）：
//
//	B - A = 2, C - B = 3, C - A = 5
func TestSolveConsistentChain(t *testing.T) {
	standards := []string{"A", "B", "C"}
	// 故意乱序提交，验证按 id 字节序而非提交顺序处理。
	records := []Record{
		{ID: "r3", From: "A", To: "C", Delta: 5},
		{ID: "r1", From: "A", To: "B", Delta: 2},
		{ID: "r2", From: "B", To: "C", Delta: 3},
	}
	res := Solve(standards, records)
	if !res.Consistent {
		t.Fatalf("expected consistent, got conflict: %+v", res.Conflict)
	}
	want := Int64Map{"A": 0, "B": 2, "C": 5}
	if !reflect.DeepEqual(res.Values, want) {
		t.Fatalf("values = %v, want %v", res.Values, want)
	}
}

// 多个连通分量分别以各自最小 id 为零点。
func TestSolveMultipleComponentsZeroedAtMin(t *testing.T) {
	standards := []string{"M", "A", "Z", "B"}
	records := []Record{
		{ID: "e1", From: "M", To: "Z", Delta: 7},  // 分量 {M,Z}，零点 M
		{ID: "e2", From: "B", To: "A", Delta: -4}, // 分量 {A,B}，零点 A
	}
	res := Solve(standards, records)
	if !res.Consistent {
		t.Fatalf("unexpected conflict: %+v", res.Conflict)
	}
	// A=0 => B=4（B-A=4）；M=0 => Z=7。
	want := Int64Map{"A": 0, "B": 4, "M": 0, "Z": 7}
	if !reflect.DeepEqual(res.Values, want) {
		t.Fatalf("values = %v, want %v", res.Values, want)
	}
}

// 三角形矛盾：B-A=2，C-B=3 隐含 C-A=5，但 r3 声称 C-A=4。
func TestSolveTriangleConflict(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "A", To: "B", Delta: 2},
		{ID: "r2", From: "B", To: "C", Delta: 3},
		{ID: "r3", From: "A", To: "C", Delta: 4}, // 首条失效记录
	}
	res := Solve(standards, records)
	if res.Consistent {
		t.Fatal("expected conflict")
	}
	c := res.Conflict
	if c.Record.ID != "r3" {
		t.Fatalf("failing record = %s, want r3", c.Record.ID)
	}
	if c.ImpliedDelta != 5 || c.Mismatch != -1 {
		t.Fatalf("implied=%d mismatch=%d, want 5/-1", c.ImpliedDelta, c.Mismatch)
	}
	if c.Path.Total != 5 {
		t.Fatalf("path total = %d, want 5", c.Path.Total)
	}
	if len(c.Path.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(c.Path.Steps))
	}
	if c.Loop.Sum != 1 { // pathTotal - recordDelta = 5 - 4
		t.Fatalf("loop sum = %d, want 1", c.Loop.Sum)
	}
	// 逐步符号与累计。
	wantSteps := []Step{
		{RecordID: "r1", From: "A", To: "B", Direction: "forward", SignedDelta: 2},
		{RecordID: "r2", From: "B", To: "C", Direction: "forward", SignedDelta: 3},
	}
	if !reflect.DeepEqual(c.Path.Steps, wantSteps) {
		t.Fatalf("steps = %+v, want %+v", c.Path.Steps, wantSteps)
	}
	if !reflect.DeepEqual(c.Path.RunningTotal, []int64{2, 5}) {
		t.Fatalf("running total = %v, want [2 5]", c.Path.RunningTotal)
	}
	wantNodes := []string{"A", "B", "C", "A"}
	if !reflect.DeepEqual(c.Loop.Nodes, wantNodes) {
		t.Fatalf("loop nodes = %v, want %v", c.Loop.Nodes, wantNodes)
	}
}

// 反向边：森林路径需要逆着某条记录行走，符号必须取反。
func TestSolveReverseEdgeInPath(t *testing.T) {
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "r1", From: "B", To: "A", Delta: -2}, // A - B = -2，即 B-A=2
		{ID: "r2", From: "C", To: "B", Delta: 5},  // B - C = 5，即 C-B=-5
		{ID: "r3", From: "A", To: "C", Delta: 0},  // 隐含 C-A = (B-2?)...
	}
	// B-A=2 => B=A+2；B-C=5 => C=B-5=A-3，故 C-A=-3。
	res := Solve(standards, records)
	if res.Consistent {
		t.Fatal("expected conflict")
	}
	c := res.Conflict
	if c.ImpliedDelta != -3 {
		t.Fatalf("implied = %d, want -3", c.ImpliedDelta)
	}
	// 路径 A->B->C：r1 反向（记录是 B->A），r2 反向（记录是 C->B）。
	if c.Path.Total != -3 {
		t.Fatalf("path total = %d, want -3", c.Path.Total)
	}
	dirs := []string{c.Path.Steps[0].Direction, c.Path.Steps[1].Direction}
	if dirs[0] != "reverse" || dirs[1] != "reverse" {
		t.Fatalf("directions = %v, want [reverse reverse]", dirs)
	}
	if c.Path.Steps[0].SignedDelta != 2 || c.Path.Steps[1].SignedDelta != -5 {
		t.Fatalf("signed = [%d %d], want [2 -5]", c.Path.Steps[0].SignedDelta, c.Path.Steps[1].SignedDelta)
	}
}

// 平行比较：同一对标准件两条记录，第二条冲突时路径只有一条边。
func TestSolveParallelComparisons(t *testing.T) {
	standards := []string{"A", "B"}
	records := []Record{
		{ID: "p1", From: "A", To: "B", Delta: 10},
		{ID: "p2", From: "A", To: "B", Delta: 11},
	}
	res := Solve(standards, records)
	if res.Consistent {
		t.Fatal("expected conflict")
	}
	c := res.Conflict
	if c.Record.ID != "p2" || c.ImpliedDelta != 10 || c.Mismatch != 1 {
		t.Fatalf("conflict = %+v", c)
	}
	if len(c.Path.Steps) != 1 || c.Path.Steps[0].RecordID != "p1" || c.Path.Total != 10 {
		t.Fatalf("path = %+v, want single p1 edge total 10", c.Path)
	}
}

// 自比较：delta=0 接受；非 0 立即矛盾。
func TestSolveSelfComparison(t *testing.T) {
	if res := Solve([]string{"A", "B"}, []Record{{ID: "s0", From: "A", To: "A", Delta: 0}}); !res.Consistent {
		t.Fatal("self delta 0 should be accepted")
	}
	res := Solve([]string{"A", "B"}, []Record{{ID: "s1", From: "A", To: "A", Delta: 3}})
	if res.Consistent {
		t.Fatal("self delta != 0 should conflict")
	}
	c := res.Conflict
	if c.Record.ID != "s1" || c.ImpliedDelta != 0 || c.Mismatch != 3 {
		t.Fatalf("self conflict = %+v", c)
	}
	if c.Path.Total != 0 || len(c.Path.Steps) != 0 {
		t.Fatalf("self conflict path should be empty, got %+v", c.Path)
	}
	if c.Loop.Sum != -3 {
		t.Fatalf("loop sum = %d, want -3", c.Loop.Sum)
	}
}

// 记录 id 按 UTF-8 字节序处理：哪一条记录“最先失效”由 id 字节序决定，
// 与提交顺序无关。同一组物理关系下，改变坏记录的 id 会改变诊断结果。
func TestSolveProcessingOrderIsRecordIDBytes(t *testing.T) {
	standards := []string{"A", "B", "C"}

	// 子测试 1：坏记录 id 排在两条好边之后，闭合时立即失效。
	t.Run("badEdgeIsLastByID", func(t *testing.T) {
		records := []Record{
			{ID: "e2-bad", From: "A", To: "C", Delta: 99}, // 坏边，id 最大
			{ID: "e0", From: "A", To: "B", Delta: 1},      // 乱序提交
			{ID: "e1", From: "B", To: "C", Delta: 1},
		}
		res := Solve(standards, records)
		if res.Consistent || res.Conflict.Record.ID != "e2-bad" {
			t.Fatalf("want e2-bad to fail, got %+v", res.Conflict)
		}
		if res.Conflict.ImpliedDelta != 2 || res.Conflict.Mismatch != 97 {
			t.Fatalf("implied=%d mismatch=%d, want 2/97",
				res.Conflict.ImpliedDelta, res.Conflict.Mismatch)
		}
		if res.Conflict.Path.Total != 2 || len(res.Conflict.Path.Steps) != 2 {
			t.Fatalf("path = %+v, want two-edge path total 2", res.Conflict.Path)
		}
	})

	// 子测试 2：同一条坏关系改用字节序最小的 id，它最先被接受（此时两端
	// 尚不同根），反而是最后处理的好边 e1 闭合时失效——报告随之改变。
	t.Run("badEdgeIsFirstByID", func(t *testing.T) {
		records := []Record{
			{ID: "00-bad", From: "A", To: "C", Delta: 99},
			{ID: "e0", From: "A", To: "B", Delta: 1},
			{ID: "e1", From: "B", To: "C", Delta: 1},
		}
		res := Solve(standards, records)
		if res.Consistent || res.Conflict.Record.ID != "e1" {
			t.Fatalf("want e1 to fail, got %+v", res.Conflict)
		}
		// e1 校验时 C-B 隐含为 C-A - (B-A) = 99-1 = 98。
		if res.Conflict.ImpliedDelta != 98 || res.Conflict.Mismatch != -97 {
			t.Fatalf("implied=%d mismatch=%d, want 98/-97",
				res.Conflict.ImpliedDelta, res.Conflict.Mismatch)
		}
		// 路径 B->A（e0 反向，-1）->C（00-bad 正向，99）累计 98。
		gotDirs := []string{res.Conflict.Path.Steps[0].Direction, res.Conflict.Path.Steps[1].Direction}
		wantDirs := []string{"reverse", "forward"}
		if !reflect.DeepEqual(gotDirs, wantDirs) {
			t.Fatalf("directions = %v, want %v", gotDirs, wantDirs)
		}
	})
}

// ---- 随机一致网络 + 注入单条冲突 ----

type genNetwork struct {
	standards []string
	values    []int64 // 真实相对值
	records   []Record
}

// genConsistentNetwork 以随机生成树为骨架构造完全一致的网络，
// 再加入若干随机弦/反向/平行/自比较记录。所有记录的 delta 由真实值导出。
func genConsistentNetwork(t *testing.T, rng *rand.Rand, n int, extra int) genNetwork {
	t.Helper()
	standards := make([]string, n)
	values := make([]int64, n)
	for i := range standards {
		standards[i] = fmt.Sprintf("S%04d", i)
		values[i] = rng.Int63n(2_000_001) - 1_000_000
	}

	parent := make([]int, n)
	for i := 1; i < n; i++ {
		parent[i] = rng.Intn(i)
	}

	var records []Record
	add := func(id string, from, to int) {
		delta := values[to] - values[from]
		if delta > maxDeltaTest || delta < -maxDeltaTest {
			t.Fatalf("generated delta %d out of test range", delta)
		}
		records = append(records, Record{ID: id, From: standards[from], To: standards[to], Delta: delta})
	}

	// 生成树边，id 随机化以强制按字节序重排。
	for i := 1; i < n; i++ {
		add(fmt.Sprintf("tree-%06d", rng.Intn(1_000_000)), parent[i], i)
	}

	// 额外记录：弦、反向边、平行边、合法自比较。
	for k := 0; k < extra; k++ {
		u := rng.Intn(n)
		v := rng.Intn(n)
		if u == v {
			records = append(records, Record{
				ID: fmt.Sprintf("self-%06d", k), From: standards[u], To: standards[u], Delta: 0,
			})
			continue
		}
		if rng.Intn(2) == 0 { // 随机翻转提交方向
			u, v = v, u
		}
		add(fmt.Sprintf("extra-%06d", k), u, v)
	}

	// 去重记录 id（极小概率随机碰撞时改名）。
	seen := map[string]bool{}
	for i := range records {
		id := records[i].ID
		for j := 0; seen[id]; j++ {
			id = records[i].ID + fmt.Sprintf("x%d", j)
		}
		seen[id] = true
		records[i].ID = id
	}

	// 乱序提交。
	rng.Shuffle(len(records), func(i, j int) { records[i], records[j] = records[j], records[i] })

	return genNetwork{standards: standards, values: values, records: records}
}

const maxDeltaTest = 1_000_000_000

func TestRandomConsistentNetworks(t *testing.T) {
	rng := rand.New(rand.NewSource(20260924))
	for iter := 0; iter < 60; iter++ {
		n := 2 + rng.Intn(40)
		net := genConsistentNetwork(t, rng, n, rng.Intn(n*2))
		res := Solve(net.standards, net.records)
		if !res.Consistent {
			t.Fatalf("iter %d: unexpected conflict on %+v", iter, res.Conflict)
		}
		checkRelativeValues(t, net, res.Values)
	}
}

// checkRelativeValues 独立验证：结果满足每条记录，且每个分量最小 id 为零点。
func checkRelativeValues(t *testing.T, net genNetwork, got Int64Map) {
	t.Helper()
	for _, r := range net.records {
		if got[r.To]-got[r.From] != r.Delta {
			t.Fatalf("record %s violated: %s=%d %s=%d delta want %d",
				r.ID, r.To, got[r.To], r.From, got[r.From], r.Delta)
		}
	}
	// 用结果自身做连通性，确认每个分量最小 id 的值为 0。
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok || p == x {
			return x
		}
		parent[x] = find(p)
		return parent[x]
	}
	for _, s := range net.standards {
		parent[s] = s
	}
	for _, r := range net.records {
		if r.From == r.To {
			continue
		}
		ra, rb := find(r.From), find(r.To)
		if ra != rb {
			parent[ra] = rb
		}
	}
	mins := map[string]string{}
	for _, s := range net.standards {
		r := find(s)
		if m, ok := mins[r]; !ok || s < m {
			mins[r] = s
		}
	}
	for _, z := range mins {
		if got[z] != 0 {
			t.Fatalf("component zero point %s has value %d, want 0", z, got[z])
		}
	}
}

// 在一致网络中注入一条冲突记录，验证：
//   - 报出的失效记录确定且是“按 id 字节序首个”矛盾；
//   - 路径累计差等于隐含差，逐步带符号相加等于总计；
//   - 环和 mismatch 可直接对照；
//   - 失效记录之前（字节序）的所有记录彼此一致。
func TestRandomInjectSingleConflict(t *testing.T) {
	rng := rand.New(rand.NewSource(424242))
	for iter := 0; iter < 120; iter++ {
		n := 3 + rng.Intn(50)
		net := genConsistentNetwork(t, rng, n, rng.Intn(n))

		// 注入记录：取两个已有标准件（含自比较情形），delta 故意错。
		from := net.standards[rng.Intn(n)]
		to := net.standards[rng.Intn(n)]
		bad := net.values[indexOf2(net.standards, to)] - net.values[indexOf2(net.standards, from)]
		perturb := int64(1 + rng.Intn(1000))
		if rng.Intn(2) == 0 {
			perturb = -perturb
		}
		// id 随机插在字节序的不同位置：前缀分布覆盖首、中、尾。
		prefix := []string{"0000-inject", "mmm-inject", "zzzz-inject"}[rng.Intn(3)]
		injected := Record{
			ID:    fmt.Sprintf("%s-%05d", prefix, iter),
			From:  from,
			To:    to,
			Delta: bad + perturb,
		}
		// 自比较且扰动后 delta 可能恰好为 0（bad=0），强制非零。
		if from == to && injected.Delta == 0 {
			injected.Delta = perturb
		}

		all := append(append([]Record{}, net.records...), injected)
		res := Solve(net.standards, all)
		if res.Consistent {
			t.Fatalf("iter %d: injected conflict not detected", iter)
		}

		verifyConflict(t, net, all, injected, res.Conflict)
	}
}

// verifyConflict 独立于求解器内部结构，复核冲突报告的每条断言。
func verifyConflict(t *testing.T, net genNetwork, all []Record, injected Record, c *Conflict) {
	t.Helper()

	// 按字节序排序全部记录，独立定位“首个失效记录”：逐条用一个朴素 DSU 复算，
	// 第一条不满足的记录必须就是报告的记录。
	sorted := append([]Record{}, all...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	d := newNaiveDSU(len(net.standards))
	pos := map[string]int{}
	for i, s := range net.standards {
		pos[s] = i
	}
	var firstFail *Record
	for i := range sorted {
		r := sorted[i]
		if !d.union(pos[r.From], pos[r.To], r.Delta) {
			r := r
			firstFail = &r
			break
		}
	}
	if firstFail == nil {
		t.Fatalf("injected record %s never caused a failure", injected.ID)
	}
	if c.Record.ID != firstFail.ID {
		t.Fatalf("reported %s, but first failing by byte order is %s", c.Record.ID, firstFail.ID)
	}

	// 报告的隐含差必须等于朴素 DSU 此刻给出的差值。
	wantImplied := d.diff(pos[c.Record.From], pos[c.Record.To])
	if c.ImpliedDelta != wantImplied {
		t.Fatalf("record %s: implied %d, naive DSU says %d", c.Record.ID, c.ImpliedDelta, wantImplied)
	}
	if c.Mismatch != c.Record.Delta-c.ImpliedDelta {
		t.Fatalf("mismatch arithmetic wrong: %d vs %d", c.Mismatch, c.Record.Delta-c.ImpliedDelta)
	}

	// 路径：端点正确，逐步有向边确实来自此前已接受的记录，
	// 带符号累加等于 RunningTotal 与 Total。
	p := c.Path
	if p.From != c.Record.From || p.To != c.Record.To {
		t.Fatalf("path endpoints %s->%s, want %s->%s", p.From, p.To, c.Record.From, c.Record.To)
	}
	var sum int64
	for i, s := range p.Steps {
		rec := findRecord(sorted, s.RecordID)
		if rec == nil {
			t.Fatalf("step record %s not found", s.RecordID)
		}
		if rec.ID >= c.Record.ID {
			t.Fatalf("path uses record %s not earlier than failing %s", rec.ID, c.Record.ID)
		}
		switch s.Direction {
		case "forward":
			if s.From != rec.From || s.To != rec.To || s.SignedDelta != rec.Delta {
				t.Fatalf("forward step inconsistent with record: %+v vs %+v", s, rec)
			}
		case "reverse":
			if s.From != rec.To || s.To != rec.From || s.SignedDelta != -rec.Delta {
				t.Fatalf("reverse step inconsistent with record: %+v vs %+v", s, rec)
			}
		default:
			t.Fatalf("bad direction %q", s.Direction)
		}
		if i > 0 && s.From != p.Steps[i-1].To {
			t.Fatal("path steps are not contiguous")
		}
		sum += s.SignedDelta
		if p.RunningTotal[i] != sum {
			t.Fatalf("running total[%d] = %d, want %d", i, p.RunningTotal[i], sum)
		}
	}
	if len(p.Steps) > 0 {
		if p.Steps[0].From != p.From || p.Steps[len(p.Steps)-1].To != p.To {
			t.Fatal("path does not span from/to")
		}
	}
	if p.Total != sum || p.Total != c.ImpliedDelta {
		t.Fatalf("path total %d, sum %d, implied %d", p.Total, sum, c.ImpliedDelta)
	}

	// 路径上的记录 id 不应重复（森林中的简单路径）。
	used := map[string]bool{}
	for _, s := range p.Steps {
		if used[s.RecordID] {
			t.Fatalf("record %s reused on path", s.RecordID)
		}
		used[s.RecordID] = true
	}

	// 环闭合：pathTotal - recordDelta 即闭合误差。
	if c.Loop.Sum != c.Path.Total-c.Record.Delta {
		t.Fatalf("loop sum %d wrong", c.Loop.Sum)
	}
	if c.Loop.Sum == 0 {
		t.Fatal("loop sum must be nonzero on conflict")
	}
	if c.Loop.PathTotal != c.Path.Total || c.Loop.RecordDelta != c.Record.Delta {
		t.Fatal("loop fields inconsistent")
	}
	if len(c.Loop.Nodes) != len(p.Steps)+2 ||
		c.Loop.Nodes[0] != p.From ||
		c.Loop.Nodes[len(c.Loop.Nodes)-1] != p.From {
		t.Fatalf("loop nodes not closed: %v", c.Loop.Nodes)
	}
}

func findRecord(rs []Record, id string) *Record {
	for i := range rs {
		if rs[i].ID == id {
			return &rs[i]
		}
	}
	return nil
}

func indexOf2(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}

// naiveDSU 是测试专用、与生产实现相互独立的朴素带权并查集（递归+无按大小合并），
// 用于交叉验证生产实现的结论。
type naiveDSU struct {
	p []int
	w []int64 // w[x] = value[x] - value[p[x]]
}

func newNaiveDSU(n int) *naiveDSU {
	d := &naiveDSU{p: make([]int, n), w: make([]int64, n)}
	for i := range d.p {
		d.p[i] = i
	}
	return d
}

func (d *naiveDSU) find(x int) int {
	if d.p[x] == x {
		return x
	}
	pp := d.p[x]
	r := d.find(pp)
	d.w[x] += d.w[pp]
	d.p[x] = r
	return r
}

// diff 返回 value[b] - value[a]（仅在同根时有意义）。
func (d *naiveDSU) diff(a, b int) int64 {
	d.find(a)
	d.find(b)
	return d.w[b] - d.w[a]
}

// union 施加约束 value[b]-value[a]=delta；若已连通且矛盾返回 false。
func (d *naiveDSU) union(a, b int, delta int64) bool {
	ra, rb := d.find(a), d.find(b)
	if ra == rb {
		return d.w[b]-d.w[a] == delta
	}
	// 朴素实现：固定把 rb 挂到 ra。
	d.p[rb] = ra
	d.w[rb] = delta + d.w[a] - d.w[b]
	return true
}

// 大规模：2000 件、6000 条记录，注入一条冲突，保证性能与可诊断性。
func TestSolveLargeNetwork(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	n, m := 2000, 6000
	net := genConsistentNetwork(t, rng, n, m-(n-1))

	// 生成器已保证 n-1 树边 + extra，裁剪/补足到 6000。
	if len(net.records) > m-1 {
		net.records = net.records[:m-1]
	}
	// 注入与真实差值确定不同的冲突记录（真实差值在 ±2e6 内，加 5e8 必矛盾）。
	trueDelta := net.values[1] - net.values[0]
	injected := Record{ID: "zzzzzzzz-conflict", From: "S0000", To: "S0001", Delta: trueDelta + 500_000_000}
	all := append(net.records, injected)

	res := Solve(net.standards, all)
	if res.Consistent {
		t.Fatal("expected conflict in large network")
	}
	verifyConflict(t, net, all, injected, res.Conflict)
}

// 非 ASCII 记录 id 也按 UTF-8 字节序处理（标准件 id 限定 ASCII，记录 id 不限）。
func TestRecordIDUTF8ByteOrder(t *testing.T) {
	// "中" 的 UTF-8 首字节 0xE4 大于所有 ASCII，故 ascii-edge 先处理。
	standards := []string{"A", "B", "C"}
	records := []Record{
		{ID: "中-edge", From: "A", To: "B", Delta: 1},
		{ID: "ascii-edge", From: "A", To: "B", Delta: 2},
	}
	res := Solve(standards, records)
	if res.Consistent {
		t.Fatal("expected conflict")
	}
	// 字节序下 ascii-edge 先被接受（A-B=2），中-edge 随后失效，隐含 2。
	if res.Conflict.Record.ID != "中-edge" || res.Conflict.ImpliedDelta != 2 {
		t.Fatalf("conflict = %+v", res.Conflict)
	}
}
