# Generation 配置契约

`si generate` 通过现有 `si.yaml` 的可选 `generation` 节点调整生成信号和低风险参数。
本文档定义该节点的字段、默认值、合并规则和无效示例。

## 合并优先级

```text
CLI flags > si.yaml > built-in defaults
```

- 未显式设置的 bool 必须与 `false` 区分：`generation.strict: false` 不会被默认值覆盖为 `true`。
- 不读取环境变量、工作目录、用户名或 Git 配置来改变生成内容。
- 未知字段、重复 key、YAML anchor/alias、timestamp 和非有限浮点数被严格拒绝，报 `GEN_INVALID_CONFIG`。
- 错误只携带字段路径（例如 `generation.metrics.histogram_buckets_seconds[2]`），不输出整个配置内容。

## 内置默认值

| 字段 | 默认值 |
| --- | --- |
| `output_dir` | `generate` |
| `signals` | `[metrics, tracing, logging]`（固定顺序） |
| `strict` | `false` |
| `metrics.namespace` | `""` |
| `metrics.histogram_buckets_seconds` | `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]` |
| `metrics.include_in_flight_gauges` | `true` |
| `metrics.max_instruments` | `10000`（硬上限 `1000000`） |
| `metrics.max_estimated_series` | `100000`（硬上限 `10000000`） |
| `metrics.summaries.enabled` | `false` |
| `metrics.summaries.quantiles` | `[0.5, 0.9, 0.99]`（仅 `enabled: true` 时读取并校验） |
| `tracing.include_internal_calls` | `false` |
| `tracing.record_exception_events` | `true` |
| `tracing.semantic_conventions_version` | `1.37.0`（内置常量，不可配置） |
| `logging.emit_start_events` | `false` |
| `logging.emit_completion_events` | `true` |
| `logging.emit_dependency_errors` | `true` |
| `logging.correlation_fields` | `[request_id, trace_id, span_id]`（allowlist） |
| `logging.redact_fields` | `[authorization, cookie, password, secret, token]`（内置 denylist 不可关闭） |

## 最小配置

省略整个 `generation` 节点即使用全部内置默认值。以下 `si.yaml` 只显式覆盖一个字段：

```yaml
service:
  name: checkout

generation:
  metrics:
    histogram_buckets_seconds: [0.01, 0.1, 1, 10]
```

## 完整配置

```yaml
service:
  name: checkout

generation:
  output_dir: generate
  signals: [metrics, tracing, logging]
  strict: false

  metrics:
    namespace: checkout
    histogram_buckets_seconds: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
    include_in_flight_gauges: true
    max_instruments: 10000
    max_estimated_series: 100000
    summaries:
      enabled: true
      quantiles: [0.5, 0.9, 0.99]

  tracing:
    include_internal_calls: false
    record_exception_events: true

  logging:
    emit_start_events: false
    emit_completion_events: true
    emit_dependency_errors: true
    correlation_fields: [request_id, trace_id, span_id]
    redact_fields: [authorization, cookie, password, secret, token, session_id]
```

## 无效示例

以下片段均产生 `GEN_INVALID_CONFIG`，错误包含对应的字段路径。

```yaml
generation:
  metrics:
    histogram_buckets_seconds: [0.5, 0.1]
# GEN_INVALID_CONFIG: generation.metrics.histogram_buckets_seconds[1]: bucket
# boundaries must be strictly increasing
```

```yaml
generation:
  signals: []
# GEN_INVALID_CONFIG: generation.signals: at least one signal is required
```

```yaml
generation:
  signals: [metrics, traces]
# GEN_INVALID_CONFIG: generation.signals[1]: unsupported signal "traces"
```

```yaml
generation:
  metrics:
    summaries:
      enabled: true
      quantiles: [0.5, 0.5]
# GEN_INVALID_CONFIG: generation.metrics.summaries.quantiles[1]: quantiles
# must be strictly increasing
```

```yaml
generation:
  logging:
    correlation_fields: [request_id, user_id]
# GEN_INVALID_CONFIG: generation.logging.correlation_fields[1]: unsupported
# correlation field "user_id"
```

```yaml
generation:
  logging:
    redact_fields: [email]
# GEN_INVALID_CONFIG: generation.logging.redact_fields: cannot disable
# built-in redaction entry "authorization"
```

```yaml
generation:
  metrics:
    namespace: "my-service"
# GEN_INVALID_CONFIG: generation.metrics.namespace: namespace must match
# [a-zA-Z_:][a-zA-Z0-9_:]*
```

```yaml
generation:
  metrics:
    max_instruments: 0
# GEN_INVALID_CONFIG: generation.metrics.max_instruments: must be a
# positive integer
```

```yaml
generation:
  semantic_conventions_version: 1.38.0
# GEN_INVALID_CONFIG: semantic_conventions_version is not a configurable
# field; the Semantic Conventions version is fixed by the built-in constant
```

## 规则摘要

- `signals` 只允许 `metrics`、`tracing`、`logging`；重复值去重，输出按固定顺序保存；空数组是配置错误。
- `output_dir` 为相对目录或显式绝对目录；不得包含 NUL；越界与 symlink 检查由 CLI Writer 执行。
- `namespace` 匹配 `[a-zA-Z_:][a-zA-Z0-9_:]*`，最长 64 字符，允许空。
- buckets 严格递增、有限且为正，数量 1 至 50。
- quantiles 在 `[0, 1]` 内、严格递增、数量 1 至 10，仅在 summaries 启用时校验。
- `correlation_fields` 只能来自 `request_id`、`trace_id`、`span_id`。
- `redact_fields` 归一化为小写并去重；内置 credential denylist 条目不可移除，只能增加。
- Policy digest 用 canonical JSON + SHA-256 计算，不包含 output path；相同语义配置在不同目录下 digest 一致。
