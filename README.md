# gaugenet

量块（标准件）成对比较校准网的一致性检查服务。纯 Go 1.23 标准库（`net/http`）
实现的 JSON API；核心是一个**带势能的并查集**，它不仅能判定整张校准网是否
自洽，还能在发现矛盾时还原出一条**确定的、由此前已接受记录组成的有向闭
环**，让复核员直接对照出错记录的符号与累计差值，而不是只收到笼统失败。

## 语义

每条比较记录的含义固定为：

```
value[to] - value[from] = delta
```

- 标准件 id：2–2000 个，唯一，非空可打印 ASCII（不含空格与控制字符）。
- 比较记录：至多 6000 条，id 唯一且为合法非空 UTF-8；`from`/`to` 必须引用
  已声明的标准件；`delta` 为整数且 `|delta| ≤ 10^9`。
- 处理顺序**严格按记录 id 的 UTF-8 字节序**（Go 字符串序），与输入数组顺序
  无关；因此“最先失效的记录”是确定的。
- 结构错误（JSON 非法、字段缺失/类型错、id 重复、引用未知标准件、超界等）
  返回 `422`，且**完全不进入计算**。
- 一致时：每个连通分量（含孤立点）以该分量内 id 最小的标准件为零点
  （值为 0），按 id 升序返回所有相对值。
- 矛盾时：返回第一条失效记录，并在**仅由此前已接受记录构成的生成森林**上
  还原 `from → to` 的唯一有向路径，逐步给出记录 id、沿行走方向的符号
  （正向边为 `delta`、反向边为 `-delta`）与累计差值。

### 矛盾报告如何对照

- `expected`：由森林路径推出的 `value[to]-value[from]`，等于 `path` 各步
  `signedDelta` 之和（即最后一步的 `cumulative`）。
- `delta`：失效记录声称的值；`residual = delta - expected ≠ 0`。
- `cycle`：`path` 之后追加一步把失效记录**反向**走回起点（`to → from`，
  贡献 `-delta`），构成一个闭合有向环；环上各步累计以 `expected - delta
  = -residual` 收尾。抄错符号的记录会让这个闭环累计明显不为 0，复核员可
  逐边核对。
- 自比较矛盾（`from == to` 且 `delta ≠ 0`）时 `path` 为空，`cycle` 仅含
  该记录反向一步，累计为 `-delta`。

## HTTP API

`POST /solve`

请求：

```json
{
  "standards": ["a", "b", "c"],
  "comparisons": [
    {"id": "e1", "from": "a", "to": "b", "delta": 2},
    {"id": "e2", "from": "b", "to": "c", "delta": 3},
    {"id": "e3", "from": "a", "to": "c", "delta": 5}
  ]
}
```

一致响应 `200`：

```json
{
  "consistent": true,
  "components": [
    {
      "zero": "a",
      "values": [
        {"standardId": "a", "value": 0},
        {"standardId": "b", "value": 2},
        {"standardId": "c", "value": 5}
      ]
    }
  ]
}
```

矛盾响应仍为 `200`（输入结构合法，这是计算结果）：

```json
{
  "consistent": false,
  "conflict": {
    "recordId": "e3",
    "from": "a", "to": "c",
    "delta": -5,
    "expected": 5,
    "residual": -10,
    "path": [
      {"from": "a", "to": "b", "recordId": "e1", "signedDelta": 2, "cumulative": 2},
      {"from": "b", "to": "c", "recordId": "e2", "signedDelta": 3, "cumulative": 5}
    ],
    "cycle": [
      {"from": "a", "to": "b", "recordId": "e1", "signedDelta": 2, "cumulative": 2},
      {"from": "b", "to": "c", "recordId": "e2", "signedDelta": 3, "cumulative": 5},
      {"from": "c", "to": "a", "recordId": "e3", "signedDelta": 5, "cumulative": 10}
    ]
  }
}
```

结构错误：`422 {"error":"..."}`；请求体超过 8 MiB：`413`。
`GET /healthz` 返回 `{"status":"ok"}`。

## 运行

```sh
# Docker Compose（构建并运行 api）
docker compose up --build
curl -fsS http://localhost:8080/healthz

# 或本地运行
go run ./cmd/api          # 默认监听 :8080，可用 PORT 覆盖

# 冒烟脚本
./scripts/smoke.sh
```

## 测试与验证

```sh
go test ./... -race -count=1
```

`internal/solver` 的测试随机生成**一致**网络（随机生成树 + 冗余边，
隐藏势值），再注入**单条**符号冲突，并覆盖：

- 反向边（同一条约束两个方向书写）；
- 平行比较（同一对标准件的多条记录、冗余一致记录）；
- 自比较（`delta=0` 一致、`delta≠0` 矛盾）；
- 输入乱序（断言结果只由记录 id 字节序决定，且可重复）；
- 20 节点的长链闭长回路（局部三角形检查无法发现的情形）；
- 每条随机用例都逐边核对路径符号、累计值、`expected`、`residual` 与闭环
  连续性。

满规模（2000 标准件 / 6000 记录）单次求解约毫秒级。

## 目录

```
cmd/api/            # net/http 服务入口
internal/api/       # JSON 解码、结构校验（422）、路由
internal/solver/    # 带势能并查集 + 已接受记录森林 + 矛盾路径还原
scripts/smoke.sh    # 运行中服务的冒烟脚本
Dockerfile          # 多阶段构建，distroless 静态镜像
docker-compose.yml  # 运行 api
```
