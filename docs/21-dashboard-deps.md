# Database、Cache 与客户端依赖 Dashboard 契约（P2-09）

P2-09 在 P2-01 Catalog、P2-02 模型、P2-03 Policy 与 P2-05 Query
Planner 之上生成 Database、Cache 与客户端依赖 category row。实现为
`dashboard/category` package 的 `BuildDependencies` / `Render`（复用
P2-08 抽取的 metric-family 聚合机制与 HTTP/RPC row 类型）。本文档链接
[Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-09）。

## 范围

- SQL Dependency 生成 Database Row；Redis Dependency 生成 Cache Row；
  HTTP Client / RPC Client Dependency 按 Policy
  `include_client_dependencies` 生成独立受控 client subsection。
- 每个 row 由对应 `DashboardItem.DependencyKind`
  （`sql` / `redis` / `http_client` / `rpc_client`）选择 item；同类依赖的
  同 Metric semantic family 可 aggregate by operation，不同
  unit/label schema 的 Metric 永不合并进同一 selector。
- 每个 row 生成：rate、error ratio、p50/p95/p99 duration stat panels
  （按 metric family 聚合）与 operation breakdown table。
- CLIENT Span capability 存在且 `include_trace_links=true` 时附加受控
  trace link；缺失时省略并记录 diagnostic。

## Row 与 Panel 标识（AC2）

| Row | Category | RowKey purpose | Row 标题 | Panel class slot / title 前缀 |
| --- | --- | --- | --- | --- |
| Database | `database` | `panels` | `Database` | `sql` / `Database` |
| Cache | `cache` | `panels` | `Cache` | `redis` / `Cache` |
| HTTP Client | `http` | `clients` | `HTTP Client Calls` | `client` / `HTTP client` |
| RPC Client | `rpc` | `clients` | `RPC Client Calls` | `client` / `RPC client` |

- Client row 的 RowKey 使用 `clients` purpose，与 P2-07 server row 的
  `panels` purpose 不冲突；client Panel 标题带 `client` operation slot，
  与 server endpoint Panel 的标题稳定区分。
- `include_client_dependencies=false` 时 client row 完全不规划，且不影响
  Database/Cache 与 server output。

## 禁止事项（AC1/AC3）

- 不读取 `Dependency.value`、`resource`、`target_service`、
  `target_url`；SQL 文本/参数、Redis key/value、HTTP URL/query/
  userinfo/Authorization、RPC target/metadata 与 PII 永不成为 label、
  title、legend、query 或 link 参数。
- 不生成 connection pool、DB instance/host/topology、retry chain 或
  原始请求值 Panel。
- label 仅限 `service`/`operation`/`status`（另加受控 `le`）；任何新
  label 均为 validation error。

## Capability / missing-panel matrix（AC1/AC4）

按每个 row 独立评估：

| Plan 能力（metric family / span） | 生成 Panel | 缺失行为 |
| --- | --- | --- |
| Counter + `service` + `operation` | rate、operations table | `DASHBOARD_MISSING_REQUIRED_METRIC`（`rows.<cat>.<class>.rate/operations`） |
| Counter + `service` + `status` | error ratio | `DASHBOARD_MISSING_REQUIRED_METRIC`（`rows.<cat>.<class>.error_ratio`） |
| Histogram + `service` | p50/p95/p99 duration | `DASHBOARD_MISSING_REQUIRED_METRIC`（`rows.<cat>.<class>.p50/p95/p99`） |
| CLIENT Span + `include_trace_links=true` | trace link | span/operation 非法 → `DASHBOARD_SENSITIVE_VALUE_DROPPED` 且省略 link；无 span → 省略 link |
| 无任何可证明 panel | row 省略 | `DASHBOARD_EMPTY_CATEGORY` + 保留 catalog `DASHBOARD_UNSUPPORTED_TARGET` |

Matrix 与 fixture（`dashboard/category/deps_test.go`）同步维护：任何
capability 规则变更必须同步更新本表与对应 fixture。

## 安全降级（AC3）

- 无 SQL/Redis/client entity：对应 row 省略，`DASHBOARD_EMPTY_CATEGORY`；
  catalog 的 `DASHBOARD_UNSUPPORTED_TARGET` /
  `DASHBOARD_SENSITIVE_VALUE_DROPPED` diagnostic 原样保留。
- 动态 target 或安全 policy 阻止：仅渲染可被 Plan 证明的 aggregate
  metric panel；无法证明时整体省略 row。
- 无效 metric name / span name / service value / operation value：
  `DASHBOARD_SENSITIVE_VALUE_DROPPED`，诊断不回显原始值。

## 限额

复用 category 上限：row ≤ 60 panels、≤ 150 queries，超过即
`DASHBOARD_PANEL_LIMIT_EXCEEDED`；单 panel target 仍为 `A`–`Z`（26）。

## 确定性

Row ID `RowIDKey(<category>, <purpose>)`；panel ID
`PanelIDKey(<category>, <class>, purpose)`；family/target 均取
canonical 最小 plan/item ID；trace links 按 URL 排序。
`BuildDependencies` / `Render` 为纯函数，不访问 clock、filesystem、
network、environment 或全局可变状态。

## 测试

- Golden：`dashboard/category/testdata/deps_golden.json` 固定四类 rows、
  panels、PromQL、trace links 与 diagnostics；`SI_UPDATE_GOLDEN=1`
  重新生成。
- SQL/Redis/client full、counter-only、no-entities、policy toggle、
  canary、permutation、query-limit 与 model validation 测试位于
  `dashboard/category/deps_test.go`。
- Canary 测试断言输出不含 SQL 参数、Redis key/value、URL
  query/userinfo、Authorization、RPC target 与 PII。
