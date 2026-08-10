# Dashboard Catalog 契约（P2-01）

本文档记录 Phase 2 的 Dashboard Catalog：把已验证的 `ObservabilityDocument` 与
`GenerationPlan` 映射为强类型、可追溯的 Dashboard Item 的契约。Catalog 是后续
Query Planner、Panel、Renderer 与 CLI 的**唯一业务输入**；任何 Query 必须经过
capability gate 才能生成。

## 1. 所有权

- 输入：`*observabilityv1.ObservabilityDocument` 与不可变 Dashboard Policy。
- Catalog builder 不读取 AST、源码路径或 Analyzer option；不导入
  `compiler/goanalyzer`、`go/ast`、`go/types`、`go/packages`。
- 不读取或复制安全策略阻止的 Dependency 值（URL、SQL、Redis key、Kafka payload）。
- 不修改输入 proto；所有集合按 category、target kind、target ID、plan ID 排序。

## 2. 分类映射（exhaustive，禁止猜测）

| 实体 | Category |
| --- | --- |
| HTTP Handler | `http` |
| gRPC Handler | `rpc` |
| Cron Job | `service_overview` |
| Kafka Producer/Consumer | `kafka` |
| SQL | `database` |
| Redis | `cache` |
| HTTP Client | `http`（仅 Policy 允许时） |
| RPC Client | `rpc`（仅 Policy 允许时） |

未知 kind 产生 `DASHBOARD_UNSUPPORTED_TARGET` Warning 并跳过，绝不猜测。

## 3. Item 内容

每个 Item 保存：

- 稳定 ID：`item:<category>:<target-id>`
- category、target ref（kind + ID）、function ID、受控 operation、安全 display name
- Metric references：Plan ID、name、type、unit、受控 label keys（仅
  `service`/`operation`/`status`）
- Span references：Plan ID、name、kind
- Capabilities：rate / error ratio / percentiles / in-flight / trace link，
  每个不可用 capability 带可解释原因
- Provenance：稳定输入路径，如 `endpoints[ep:http]`、`dependencies[dep:sql]`
  （ID 派生，与输入顺序无关）

## 4. Capability Gate

| Capability | 必需 Plan 项 |
| --- | --- |
| rate | Counter + `service` + `operation` attribute |
| error ratio | 任一 Metric 声明 `status` attribute |
| percentiles | Histogram |
| in-flight | Gauge |
| trace link | 适配的 Span Plan |

无依据的 capability 标记不可用并给出原因；后续 Planner 不生成对应 Panel。

## 5. 错误与 Diagnostic

- Fatal（返回 `DASHBOARD_INVALID_IR` / `DASHBOARD_DANGLING_REFERENCE` /
  `DASHBOARD_UNSUPPORTED_SCHEMA`，无部分 Catalog）：
  nil document/plan、未知 schema、重复 Plan ID、Plan target 指向缺失或类型不匹配的实体。
- Warning（进入 Catalog.Diagnostics）：无法安全映射的 entity kind；
  strict Policy 下提升为失败。
- 敏感值结构性不读取：canary 值不会出现在 Catalog、Diagnostic 或错误中。

## 6. 确定性

- 输入排列（25 个固定排列）不影响排序后的 Catalog、capability 与 diagnostics。
- 所有 ID 由稳定值派生，无随机 UUID 或计数器。
