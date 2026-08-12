# Service Overview 与受控变量契约（P2-06）

P2-06 在 P2-01 Catalog、P2-02 模型、P2-03 Policy 与 P2-05 Query
Planner 之上实现 Service Overview Row 与受控变量。实现为
`dashboard/overview` package：`Build` 产出 typed `Plan`（variables、
panels、family targets、diagnostics），`Render` 将其转换为 P2-02
`model.Variable` / `model.Panel`。本文件链接
[Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-06）。

## 变量

- 唯一 datasource variable：name `datasource`（Policy v1 固定）、type
  `datasource`、query `prometheus`、`hide: 0`；不含 URL、UID 或
  credential。
- operation variable 仅在 Catalog 声明 ≥2 个有效低基数 operation 时生成：
  type `custom`，选项来自 validated normalized operation，canonical 排序，
  静态 options；`multi=true`、`includeAll=true`，禁止 `label_values`。
- 无效 operation 值以 `DASHBOARD_SENSITIVE_VALUE_DROPPED` 丢弃并记录
  diagnostic，绝不进入变量或 selector。

## 面板

固定顺序与形状：

| Purpose | 类型 | 宽 | 单位 | noValue |
| --- | --- | ---: | --- | --- |
| `requests_rate` | stat | 6 | `ops/s` | `0` |
| `error_ratio` | stat | 6 | `percent` | `0` |
| `p50` / `p95` / `p99` | stat | 6 | `s` | `0` |
| `in_flight` | stat | 6 | `short` | `0` |
| `top_failing` | table | 24 | `ops/s` | `0` |

每个 Panel 固定 title、description、unit、legend、no-value 与 target；
`description`、`noValue`、`hide` 与 overview query metadata 由 P2-02 契约
扩展承载（见 [14-dashboard-json-contract.md](14-dashboard-json-contract.md)）。

## Metric family 与查询

- 一个 metric family = 相同 `MetricPlan.name` + metric type + label schema
  （`service`/`operation`/`status` 属性集合）。跨 category 聚合只允许同一
  family；不同 name/label schema 绝不混入同一 selector。
- rate / error ratio / top failing：counter family（service+operation /
  service+status）。percentile：histogram family（service）。in-flight：
  gauge family（service）。
- 表达式与 P2-05 完全一致（`rate`、`histogram_quantile`、固定 error-status
  regex、`le!="+Inf"`），每个 target 均通过 `query.ValidateOverviewPlan`
  对全部聚合 item 的交叉校验。
- overview target metadata 记录 `categories` / `item_ids` / `plan_ids`，
  供 renderer 与 future runbook consumer 追溯。

## 失败与诊断

- Overview 总 Query 数上限 `MaxOverviewQueries = 30`；超过即失败
  `DASHBOARD_PANEL_LIMIT_EXCEEDED`，禁止截断。
- 无适用 family 的 panel 省略并记录 `DASHBOARD_MISSING_REQUIRED_METRIC`
  （field `overview.panels.<purpose>`）。
- 无任何 panel 时省略 Overview Row 并记录 `DASHBOARD_EMPTY_CATEGORY`；
  Dashboard 层（P2-10）保证至少有一个可用 panel，否则命令失败。
- 无效 metric name / operation / service value 全部
  `DASHBOARD_SENSITIVE_VALUE_DROPPED`，诊断不回显原始值。

## 确定性

Panel ID 使用 `PanelIDKey(service_overview, "overview", purpose)` +
`ResolvePanelIDs`；target refId 使用 canonical key 排序 +
`AllocateRefIDs`；grid 使用 P2-04 规则（24 列、stat 6、table 24、按 ID
升序换行）。`Build`/`Render` 均为纯函数，不访问 clock、filesystem、
network、environment 或全局可变状态。

## 测试

- Golden：`dashboard/overview/testdata/overview_golden.json` 固定 variables、
  panel 形状、PromQL、family references 与 diagnostics；`SI_UPDATE_GOLDEN=1`
  重新生成。
- AC1 复合 service、AC2 counter-only、AC3 canary、query 上限、permutation
  确定性与 import boundary 测试位于 `dashboard/overview`。
- P2-05 交叉校验矩阵位于 `dashboard/query/overview_validate_test.go`。
