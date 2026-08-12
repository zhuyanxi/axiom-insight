# Kafka Dashboard 契约（P2-08）

P2-08 在 P2-01 Catalog、P2-02 模型、P2-03 Policy 与 P2-05 Query
Planner 之上生成 Kafka category row。实现为 `dashboard/category` package
的 `BuildKafka` / `Render`（复用 HTTP/RPC row 类型与 renderer）。本文档
链接 [Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-08）。

## 范围

- 仅从 `KAFKA_PRODUCER` / `KAFKA_CONSUMER` Dependency 的 Phase 1
  Metric/Span Plan 建立 item；`DashboardItem.DependencyKind` 区分
  producer/consumer 两类（P2-01 catalog 契约新增字段）。
- 同一 Kafka Row 内以受控 subtitle（panel title 前缀 `Kafka producer` /
  `Kafka consumer`）分别呈现两类操作。
- Producer 与 consumer 各生成：request rate、error ratio、
  p50/p95/p99 duration stat panels（按 metric family 聚合）与
  operation breakdown table。
- PRODUCER/CONSUMER Span capability 存在且 `include_trace_links=true`
  时附加受控 trace link；缺失时省略并记录 diagnostic。

## 禁止事项（AC2）

- 不读取 `Dependency.value`、`resource`、`target_service`、`target_url`；
  topic、consumer group、partition、offset、payload 与 message body 永不
  成为 label、title、legend、query 或 link 参数。
- 不生成 consumer handler root、lag、offset 或 partition Panel。
- label 仅限 `service`/`operation`/`status`；查询只引用 Plan metric/labels。

## 安全降级（AC3）

- 无 Kafka entity：row 省略，`DASHBOARD_EMPTY_CATEGORY`；catalog 的
  `DASHBOARD_UNSUPPORTED_TARGET` / `DASHBOARD_SENSITIVE_VALUE_DROPPED`
  diagnostic 原样保留。
- 动态 target 或安全 policy 阻止：仅渲染可被 Plan 证明的 aggregate panel；
  无法证明时整体省略 row。
- 无效 metric name / span name / service value：
  `DASHBOARD_SENSITIVE_VALUE_DROPPED`，诊断不回显原始值。

## 限额

复用 category 上限：row ≤ 60 panels、≤ 150 queries，超过即
`DASHBOARD_PANEL_LIMIT_EXCEEDED`；单 panel target 仍为 `A`–`Z`（26）。

## 确定性

Row ID `RowIDKey(kafka, "panels")`；panel ID
`PanelIDKey(kafka, producer|consumer, purpose)`；family/target 均取
canonical 最小 plan/item ID；trace links 按 URL 排序。`BuildKafka` /
`Render` 为纯函数，不访问 clock、filesystem、network、environment 或
全局可变状态。

## 测试

- Golden：`dashboard/category/testdata/kafka_golden.json` 固定 row、两类
  panels、PromQL、trace links 与 diagnostics；`SI_UPDATE_GOLDEN=1`
  重新生成。
- Producer/consumer/mixed、counter-only、dynamic（no entities）、canary、
  permutation、query-limit 与 model validation 测试位于
  `dashboard/category/kafka_test.go`。
- Canary 测试断言输出不含 `topic`/`group`/`partition`/`offset`/`payload`
  及注入值。
