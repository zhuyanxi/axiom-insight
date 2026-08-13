# Dashboard 渲染与验证契约（P2-10）

P2-10 在 P2-06 Overview、P2-07 HTTP/RPC、P2-08 Kafka、P2-09
dependency 之上实现确定性组装与渲染。实现为 `dashboard/pipeline`
package：`Build` 组装不可变 `Plan`，`Render` 输出 canonical JSON 与
definition hash/count，`Diff` 提供 Golden 语义 diff。本文档链接
[Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-10）。

## 固定调用链

```text
P2-01 Catalog -> overview.Build/Render + category.Build/BuildKafka/
BuildDependencies/Render
  -> pipeline.Build（组装 + 排序 + 行堆叠 + 限额 + digest）
  -> pipeline.Render（render 前 Validate -> model.Render
     -> 严格 Decode + 重新 Validate -> SHA-256 + counts）
  -> CLI Report（P2-11/P2-12 消费）
```

## Plan（不可变）

`pipeline.Plan` 保存 service、title、UID、policy digest、timezone、
refresh、templating variables、stacked rows 与排序去重的 diagnostics；
通过 accessors 只读访问，slices 返回防御性拷贝。Policy digest 来自
`policy.Digest()`（P2-03），只进 CLI report，绝不写入 dashboard JSON。

## 组装与布局

- Row 顺序固定：Service Overview、HTTP、RPC、Kafka、Database、Cache、
  HTTP Client Calls、RPC Client Calls；Overview 的 P2-06 顶层 panels 被
  包装为 Service Overview Row（model 契约要求 Rows 与顶层 Panels 互斥）。
- 空 row 整体省略；所有 row ID 在单一 `ResolvePanelIDs` 域内重新分配，
  保证跨子渲染器唯一。
- 每行 panels 携带相对 Y（自 0 起）；`stackRows` 按行带 + 内容高度向下
  堆叠，任何 panel 满足 $0 \le Y$、$0 \le X$、$X + W \le 24$、$1 \le W \le 24$。
- Renderer 不重新推断 capability、不重写 query semantics；它只组合
  P2-06..P2-09 的输出。

## 验证与渲染（AC1/AC2）

1. `model.Validate`（render 前）；
2. `model.Render` canonical JSON（two-space、trailing LF、无 HTML
   escaping、无 timestamp/server ID）；
3. 渲染后 `model.Decode` 严格解码 + `model.Validate` 重新校验；
4. 任一失败返回 `DASHBOARD_RENDER_ERROR`，不返回 bytes 或部分 model；
5. 全空 dashboard（无任何 non-row panel）在 `Build` 阶段以
   `DASHBOARD_EMPTY_CATEGORY` 失败；
6. 超出 Policy `max_panels` / `max_queries` 以
   `DASHBOARD_PANEL_LIMIT_EXCEEDED` 失败，禁止截断。

## Hash 与 counts（AC1/AC3）

`Result` 携带 `SHA256`（rendered bytes 的 hex SHA-256）、`RowCount`、
`PanelCount`、`QueryCount`；Golden 测试固定这些值与 row 顺序。

## 语义 diff（P2-10 task 7）

`Diff(expected, actual []byte) ([]DiffEntry, error)` 严格解码两份文档后
按 canonical key 顺序遍历，报告每个差异的 JSON path、所属 Panel ID 与
两个标量值；输出上限 100 条，避免发散 fixture 爆炸。Golden 失败时据此
定位 Dashboard path / Panel ID / Query metadata，而非原始 bytes diff。

## 禁止事项

- 输出不含 policy digest、absolute path、timestamp、host、user、secret
  或 runtime ID；
- 不访问 clock、filesystem、network、environment 或全局可变状态；
- 不导入 Analyzer/AST/plugin/compiler package（import boundary test）。

## 测试

- `plan_test.go`：full/empty/limits/determinism/grid-stacking/client
  toggle；
- `render_test.go`：canonical、determinism、invalid plan 无部分输出、
  no-secrets；
- `diff_test.go`：identical/controlled change/service change/invalid
  input/no-map-order；
- `golden_test.go`：`testdata/pipeline_golden.json` 固定 SHA-256、counts
  与 row 顺序；`SI_UPDATE_GOLDEN=1` 重新生成；
- `import_boundary_test.go`：pipeline 不传递导入禁用 package。

Golden 更新只能显式 opt-in（`SI_UPDATE_GOLDEN=1`），PR 需解释每个语义
变化。
