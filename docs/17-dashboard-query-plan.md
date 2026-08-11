# Dashboard Query Plan 契约（P2-05）

SRE 需要的每个 Dashboard Panel 都来自已声明的 Metric/Span Plan：由
`dashboard/query` package 从 Catalog（P2-01）、Policy（P2-03）与确定性命名层
（P2-04）构建可审查的 typed query plan，再经结构化 Renderer 输出受控 PromQL
子集。本文档链接 [Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-05）。

## 构建规则

| Query 类型 | 前置能力 | 表达式 |
| --- | --- | --- |
| rate | Counter + `service`/`operation` attribute | `sum by (operation) (rate(<counter>{service=...,operation=...}[$__rate_interval]))` |
| error_ratio | Counter + `status` attribute | 分子/分母同一 selector domain；分子仅追加固定 `status=~"5[0-9]{2}\|error"` |
| percentile (p50/p95/p99) | Histogram + `service`/`operation` | `histogram_quantile(q, sum by (le) (rate(<hist>{le!="+Inf",...}[$__rate_interval])))` |
| in_flight | Gauge | `sum by (operation) (<gauge>{...})`；禁止 `rate()` 与 status matcher |
| operation_breakdown | Counter | 每个受控 operation value 一条 rate query；超过固定上限 32 条报 `DASHBOARD_PANEL_LIMIT_EXCEEDED`，禁止截断 |
| trace_link | `include_trace_links=true` + SpanPlan | URL model 仅含固定 datasource variable、service、operation、span name；不携带 trace/request ID、host 或外部 URL |

- quantile 顺序固定 `0.50, 0.95, 0.99`；`$__rate_interval` 固定（Policy 校验）。
- 能力缺失不产生替代 Query：一律 `DASHBOARD_MISSING_REQUIRED_METRIC`。
- 同一服务名视为同一实体；归一化后冲突名由 P2-04 消歧。

## 受控输入门（Canary 防护）

未验证输入无法成为 metric/label/regex/URL 片段，全部以
`DASHBOARD_SENSITIVE_VALUE_DROPPED` 丢弃（诊断不回显被拒绝的值）：

- metric name：`[A-Za-z_:][A-Za-z0-9_:]{0,199}`（Prometheus 字符集）。
- service matcher value：`[A-Za-z0-9_.-]{1,128}`（允许点号域名式服务名）。
- operation value：`[A-Za-z0-9_-]{1,128}`（machine-name 形状，无点号）。
- span name：`[A-Za-z0-9_. /-]{1,128}`；引号、换行、控制字符、操作符被拒。
- 正则仅允许固定 `5[0-9]{2}|error`；`le` matcher 固定为 `le!="+Inf"`。
- 渲染全部经 `strconv.Quote`；parser 拒绝 escape 序列与未知字节。

## 支持的子集

```
expr       := term ( "/" term )*
term       := "sum" "by" "(" labels ")" "(" expr ")"
            | "rate" "(" selector "[" interval "]" ")"
            | "histogram_quantile" "(" number "," expr ")"
            | selector | number | "(" expr ")"
selector   := metric_name ( "{" label op value ( "," label op value )* "}" )?
op         := "=" | "!=" | "=~"
```

- 不允许：bare selector 根、动态 label 名、任意聚合（仅 `sum`）、
  任意函数（仅 `rate`/`histogram_quantile`）、注释、用户正则、
  原始 PromQL、`label_values`。
- Renderer 只消费 typed nodes；`fmt.Sprintf` 不接触外部内容。

## 可追溯性（AC1）

每个 `QueryPlan` 携带 `CanonicalKey`（`query:<kind>:<itemID>:<purpose>`）、
`PlanIDs`（来源 Metric/SpanPlan ID）与 `QueryMetadata`（kind、canonical key、
plan IDs、provenance、rate interval、quantiles、error-status pattern、
operation values、hash version）。Renderer 只把 `expr` 输出为 Grafana JSON；
metadata 永不序列化。

## Validator（AC7/Cross-check）

`ValidatePlan` 校验每个 selector metric name 与 item 声明的 MetricPlan 精确
一致、matcher label 属于词汇表 `{service, operation, status, le}`、聚合与
函数匹配 metric 类型（gauge 禁 `rate`、in-flight 禁 status）、错误率两侧
selector domain 一致。任何 Plan metric name/attribute 漂移都会使旧 Query
validation 失败（cross-check test 固定该行为）。

## 测试与 Golden

- 表驱动 query matrix：Counter/Histogram/Gauge × 每 category × 缺失 attribute
  × max query cases。
- Fuzz：parser 任意输入不 panic；parse→render→parse 语义稳定。
- Golden：`dashboard/query/testdata/query_plans_golden.json` 固定全部 PromQL、
  metadata 与 TraceLink plan；`SI_UPDATE_GOLDEN=1` 重新生成，重跑无 diff。
- 复杂度 `O(Q)`，Q 为 Query 数；不引入 Prometheus/Grafana client、网络库或
  正则拼接 API。
