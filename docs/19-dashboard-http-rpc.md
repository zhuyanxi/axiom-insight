# HTTP 与 gRPC Dashboard 契约（P2-07）

P2-07 在 P2-01 Catalog、P2-02 模型、P2-03 Policy 与 P2-05 Query
Planner 之上生成 HTTP 与 RPC category rows。实现为
`dashboard/category` package：`Build` 产出 typed `Plan`（rows、panels、
targets、diagnostics），`Render` 转换为 P2-02 `model.Row`。本文档链接
[Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-07）。

## 行与面板

- HTTP Handler endpoint → `http` row；gRPC Handler endpoint → `rpc` row。
  HTTP/RPC Client dependency 由 P2-09 处理，绝不进入本 story 的 row。
- 每个 endpoint 按 catalog capability 生成：request rate、error ratio、
  p50/p95/p99 duration、in-flight stat panels（宽 6），以及每 category 一个
  operation breakdown table（宽 24）。
- 每个 row 携带静态 description：Phase 1 Instrumentation Plan 来源 +
  runtime instrumentation 需求；不显示用户操作说明或敏感实现细节。
- SERVER Span capability 存在且 `include_trace_links=true` 时，endpoint
  每个 panel 附加受控 trace link（`/d/${uid}` 内部链接，仅含 datasource
  variable 与 validated service/operation/span 值）；缺失时省略并记录
  diagnostic，绝不生成虚假 Tempo query。

## 维度来源（AC2）

- title、legend、query 只使用 Phase 1 normalized operation；原始 path、
  gRPC metadata、SQL/URL/key 等原始值不进入任何可见层。
- label 仅限 `service`/`operation`/`status`；不生成 endpoint-specific raw
  value variable 或正则 matcher。
- 多个 endpoint 共享 metric family 时，operation table 按 family 聚合、
  plan ID/target ID 取 canonical 最小者，metadata 稳定且与输入排列无关。

## 降级与诊断（AC3）

- 能力缺失：percentile/in-flight/trace link 省略，PlanItemQueries 产生
  `DASHBOARD_MISSING_REQUIRED_METRIC`。
- 无实体：row 整体省略并产生 `DASHBOARD_EMPTY_CATEGORY`（field
  `rows.http` / `rows.rpc`）。
- 安全策略阻止：无效 metric name、span name、service value 一律
  `DASHBOARD_SENSITIVE_VALUE_DROPPED`，诊断不回显原始值。

## 限额

- 每 category 最多 `MaxPanelsPerCategory = 60` panels、
  `MaxQueriesPerCategory = 150` queries；超过即失败
  `DASHBOARD_PANEL_LIMIT_EXCEEDED`，禁止截断。
- 单 panel target 数沿用 P2-04 `A`–`Z` 上限（26），由
  `dashboard.AllocateRefIDs` 强制。

## 确定性

Row ID 使用 `RowIDKey(category, "panels")`；Panel ID 使用
`PanelIDKey(category, itemID, purpose)`；grid 由 `dashboard.PlanGrid`
按 P2-04 规则放置（row panel 行偏移减 1，nested panels 从 y=0 开始）。
`Build`/`Render` 均为纯函数，不访问 clock、filesystem、network、
environment 或全局可变状态。

## 测试

- Golden：`dashboard/category/testdata/category_golden.json` 固定 rows、
  panels、PromQL、trace links 与 diagnostics；`SI_UPDATE_GOLDEN=1`
  重新生成。
- AC1 full、AC2 canary、AC3 counter-only、AC4 permutation、panel limit
  与 import boundary 测试位于 `dashboard/category`。
- 每个 per-item target 经 `query.ValidatePlan` 交叉校验；operation
  table target 经 `query.Render` + `query.Parse` round-trip 校验。
