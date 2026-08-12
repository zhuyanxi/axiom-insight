# `dashboard.json` JSON 契约（`grafana.dashboard/v1`）

本文档记录 Phase 2 的 Grafana Dashboard JSON v1 契约（P2-02）。该 JSON 是
Grafana dashboard API/export model 的离线子集，固定 Grafana Schema **41**；
不依赖运行中的 Grafana 即可严格校验。

## 1. 固定约束

- `schemaVersion` 固定 `41`；`id` 固定 `null`；`version` 固定 `0`；`editable` 固定 `true`。
- Panel type 仅允许 `timeseries`、`stat`、`gauge`、`table`、`row`；拒绝 text、
  plugin type、未识别 type 与任意 options passthrough。
- 所有 target 的 datasource 为受控常量 `${datasource}`，`format` 为 `time_series`，
  `refId` 为 `A`–`Z` 受控分配；单 Panel target 数上限 26。
- Panel 可携带静态 `description`（≤255 字符）与
  `fieldConfig.defaults.noValue`（≤64 字符）；datasource variable 可显式携带
  `hide: 0`（可见）。
- Row 可携带静态 `description`（≤255 字符），说明 Phase 1 Instrumentation
  Plan 来源与 runtime instrumentation 需求；不包含用户操作说明或敏感实现细节。
- Query metadata 有两种受控形态：per-item 形态
  `{plan_id, target_id, kind}`，以及 Overview 聚合形态
  `{kind, categories, item_ids, plan_ids}`（P2-06 跨 category 聚合 Query 使用；
  两形态互斥，不能只填 `plan_id`/`target_id` 之一，也不能同时混用两形态；
  `categories`/`item_ids`/`plan_ids` 的数组条目均不得为空字符串）。
- 顶层与 Panel 一律使用 typed model，禁止 `map[string]any` 与任意 options。
- 禁止生成时间、随机 UUID、服务器 ID、主机名、绝对路径、`__inputs`/`__requires`、
  HTML/JavaScript 与外部数据源 endpoint/token/tenant/credential。

## 2. 渲染示例

以下 JSON 是 `dashboard/model.Render` 的合法输出形态：

```json
{
  "schemaVersion": 41,
  "title": "checkout Observability",
  "uid": "checkout-observability",
  "id": null,
  "version": 0,
  "editable": true,
  "timezone": "browser",
  "refresh": "30s",
  "templating": {
    "list": [
      { "name": "datasource", "type": "datasource", "query": "prometheus" },
      { "name": "service", "type": "query", "datasource": { "type": "prometheus", "uid": "${datasource}" }, "query": "checkout_http_requests_total" }
    ]
  },
  "rows": [
    {
      "id": 101,
      "title": "HTTP",
      "panels": [
        {
          "id": 1,
          "title": "HTTP request rate",
          "type": "timeseries",
          "gridPos": { "x": 0, "y": 0, "w": 12, "h": 8 },
          "datasource": { "type": "prometheus", "uid": "${datasource}" },
          "targets": [
            {
              "refId": "A",
              "datasource": { "type": "prometheus", "uid": "${datasource}" },
              "expr": "sum(rate(checkout_http_requests_total[$__rate_interval])) by (operation)",
              "format": "time_series",
              "legendFormat": "{{operation}}",
              "metadata": { "plan_id": "metrics:ep:http:count", "target_id": "ep:http", "kind": "rate" }
            }
          ],
          "fieldConfig": { "defaults": { "unit": "ops" } }
        }
      ]
    }
  ],
  "links": [],
  "annotations": { "list": [] }
}
```

## 3. 校验规则

- Decoder 拒绝未知字段、重复 key、非有限数值、超过 10 MiB、超过 64 层嵌套、
  无效 UTF-8 与 `__inputs`/`__requires`，错误含 JSON path。
- Semantic Validator 校验 UID 字符集/长度、title、Panel/Row ID 唯一性与正值、
  grid 边界（`0≤x`、`0≤y`、`1≤w,h≤24`、`x+w≤24`）、row/panel 嵌套互斥、
  target refId `A`–`Z` 唯一、datasource 必须为 `${datasource}` 且 templating 声明
  保留变量、query metadata 完整、link 仅限相对/内部 URL。
- 机器可读 Schema：`schemas/dashboard/v1/grafana-dashboard.schema.json`，
  关闭 `additionalProperties`，为可选对象设定明确上限；Go Validator 与 Schema
  对同一 fixture 结论一致（重复 key 与跨对象 ID 唯一性为 Go 专属规则）。

## 4. 封闭规则

`grafana.dashboard/v1` 发布后字段集合固定。新增、删除、重命名字段，或升级
Grafana Schema，必须新建 `grafana.dashboard/vN` 并重新执行导入兼容性测试；
不得以 optional field 绕过版本协商。Reader 只接受声明支持的精确版本并拒绝
未知字段。

## 5. 兼容性语料

`testdata/dashboard/corpus/*.json` 覆盖 row、timeseries、stat、gauge、table、
变量与 datasource reference 的导入必需字段；语料在本地完成 decode →
validate → render → re-decode 全链路，不连接 Grafana。
