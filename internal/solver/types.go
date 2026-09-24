// Package solver 维护带势能（potential）的并查集，按记录 id 的 UTF-8 字节序
// 逐条检查比较记录，并在发现首个矛盾时从“此前已接受记录”构成的森林中
// 还原可直接对照的有向路径。
package solver

// Record 是一条比较记录，固定含义：value[to] - value[from] = Delta。
type Record struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Delta int64  `json:"delta"`
}

// Step 是有向路径上的一步。
type Step struct {
	RecordID string `json:"recordId"`
	From     string `json:"from"`
	To       string `json:"to"`
	// Direction 为 "forward" 表示按记录方向行走（to - from = delta），
	// 为 "reverse" 表示反向行走（from - to = -delta）。
	Direction string `json:"direction"`
	// SignedDelta 是沿行走方向的累计增量：正向为 delta，反向为 -delta。
	SignedDelta int64 `json:"signedDelta"`
}

// Path 是从 From 到 To 的有向路径，RunningTotal 为对应步的累计差值
// （RunningTotal[k] = 前 k+1 步 SignedDelta 之和，最后一个等于 Total）。
type Path struct {
	From         string  `json:"from"`
	To           string  `json:"to"`
	Steps        []Step  `json:"steps"`
	RunningTotal []int64 `json:"runningTotal"`
	Total        int64   `json:"total"`
}

// Loop 是记录 id 顺序上最先闭合的矛盾环：先走森林路径，再由失效记录闭合。
type Loop struct {
	Nodes       []string `json:"nodes"`
	PathTotal   int64    `json:"pathTotal"`
	RecordDelta int64    `json:"recordDelta"`
	// Sum 为沿闭合环一圈的代数和，PathTotal - RecordDelta；一致时必为 0。
	Sum int64 `json:"sum"`
}

// Conflict 描述最先失效的记录及其与森林路径的对照信息。
type Conflict struct {
	Record Record `json:"record"`
	// ImpliedDelta 是此前已接受记录所隐含的 value[to] - value[from]。
	ImpliedDelta int64 `json:"impliedDelta"`
	// Mismatch 为 Record.Delta - ImpliedDelta，非零即矛盾。
	Mismatch int64 `json:"mismatch"`
	Path     Path  `json:"path"`
	Loop     Loop  `json:"loop"`
}

// Result 是一次约束传播的结论。Consistent 为 true 时 Values 有效；
// 为 false 时 Conflict 指向第一条失效记录。
type Result struct {
	Consistent bool      `json:"consistent"`
	Values     Int64Map  `json:"values,omitempty"`
	Conflict   *Conflict `json:"conflict,omitempty"`
}
