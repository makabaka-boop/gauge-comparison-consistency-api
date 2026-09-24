# gaugeblock — 量块校准网一致性检查

计量室用成对比较传递量块相对偏差。一条抄反符号的记录会让整张校准网
自相矛盾，而只检查局部三角形会漏掉更长的闭合回路。本服务把每条比较记录
解释为一个等式约束

```
value[to] - value[from] = delta
```

用带势能（potential）的并查集做全局约束传播，**按记录 id 的 UTF-8 字节序**
逐条处理：全部一致时给出以每个连通分量最小标准件为零点的全部相对值；
一旦矛盾，定位到**最先失效的记录**，并从“此前已接受记录”构成的森林里
还原 `from → to` 的有向路径、逐步符号与累计差值，复核员可以把路径总差
和失效记录的 delta 直接对照，得到一个确定的矛盾闭环。

## 技术栈

- Go 1.23，仅标准库 `net/http`，纯后端 JSON API，无外部依赖
- Docker / Docker Compose 运行（多阶段构建，distroless 非 root 镜像）
- 测试使用与生产实现相互独立的朴素带权并查集交叉验证

## 目录结构

```
cmd/api/             服务入口（PORT 环境变量，默认 8080）
internal/solver/     带势能并查集 + 森林路径/矛盾环诊断
internal/api/        JSON API 与结构校验（422）
Dockerfile           多阶段构建
docker-compose.yml   api 服务
```

## 运行

```bash
docker compose up --build
# 或本地
go run ./cmd/api
```

## API

### `GET /healthz`

```json
{"status":"ok"}
```

### `POST /check`

请求：

| 字段 | 说明 |
| --- | --- |
| `standards` | 2–2000 个唯一 ASCII 标准件 id（非空可见 ASCII，0x21–0x7e） |
| `records` | 至多 6000 条比较，记录 id 唯一；`from`/`to` 必须引用已有标准件 |
| `records[].delta` | 整数，绝对值 ≤ 10^9 |

结构错误（JSON 非法、未知字段、数量越界、id 重复、端点不存在、
delta 越界等）返回 **422**，且不进入任何计算。

一致时（HTTP 200）：

```json
{
  "consistent": true,
  "values": {"A": 0, "B": 2, "C": 5}
}
```

`values` 以每个连通分量中 id 最小的标准件为零点，键按 id 排序输出。

矛盾时（HTTP 200，业务结果）：

```json
{
  "consistent": false,
  "conflict": {
    "record": {"id": "r5", "from": "A", "to": "E", "delta": 20},
    "impliedDelta": 18,
    "mismatch": 2,
    "path": {
      "from": "A",
      "to": "E",
      "steps": [
        {"recordId": "r1", "from": "A", "to": "B", "direction": "forward", "signedDelta": 1},
        {"recordId": "r2", "from": "B", "to": "C", "direction": "reverse", "signedDelta": 3},
        {"recordId": "r3", "from": "C", "to": "D", "direction": "forward", "signedDelta": 10},
        {"recordId": "r4", "from": "D", "to": "E", "direction": "reverse", "signedDelta": 4}
      ],
      "runningTotal": [1, 4, 14, 18],
      "total": 18
    },
    "loop": {
      "nodes": ["A", "B", "C", "D", "E", "A"],
      "pathTotal": 18,
      "recordDelta": 20,
      "sum": -2
    }
  }
}
```

对照方法：沿 `path` 走一圈再由失效记录闭合，`loop.sum = pathTotal - recordDelta`
非零即为闭合误差；`steps[].direction` 为 `reverse` 时符号已取反
（`signedDelta = -delta`），所以逐步 `signedDelta` 累加必然等于 `total`。
自比较矛盾（`from == to` 且 delta 非 0）时路径为空，环退化为单点环。

## 算法要点

- **带势能并查集**：`pot[x] = value[x] - value[parent]`，路径压缩时累加势能；
  按大小合并。同根记录立即用 `pot[to] - pot[from]` 校验，跨分量记录合并。
- **确定性的“最先失效”**：记录先按 id 的 UTF-8 字节序排序再处理，
  与提交顺序无关。
- **诊断用的森林**：只有真正连接两个分量的已接受记录进入无向森林，
  因此任意两端之间路径唯一；同根的平行比较/闭合弦不进森林，不会污染路径。
- **零点选择**：每个连通分量以 id 最小的标准件为 0，相对值为
  `pot[x] - pot[zero]`。

## 测试

```bash
go test -race ./...
```

覆盖：

- 随机生成以生成树为骨架的一致网络（含随机弦、反向边、平行边、
  合法自比较），独立复核每条约束与各分量零点；
- 注入单条冲突，用**独立的朴素带权并查集**确认报告的就是字节序下
  首条失效记录，并逐步复核路径方向、符号、累计差与闭合环；
- 反向边、平行比较、自比较、输入乱序、记录 id 的 UTF-8 字节序；
- 2000 标准件 / 6000 记录的大规模场景；
- HTTP 层全部结构错误均为 422。
