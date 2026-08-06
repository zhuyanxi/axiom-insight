# `otel.yaml` 文件契约与渲染规则

本文档记录 `si generate` 输出的 `otel.yaml`（P1-11）契约示例、字段到 Runtime
binding source 的映射和渲染规则。**该文件是 Instrumentation Plan，不是
OpenTelemetry Collector 配置**：顶层 `plan_kind: instrumentation` 明确标识；Schema
不允许 receivers、processors、exporters 或 service.pipelines 字段。

## 1. 渲染示例

以下示例是 `RenderTracingPlan(plan, policy)` 的真实输出格式（Golden fixture
`testdata/golden/otel_minimal.yaml`）：

```yaml
schema_version: generator.otel/v1
document_type: instrumentation.tracing
plan_kind: instrumentation
semantic_conventions_version: 1.37.0
source:
  ir_schema_version: v1
  service_name: orders
generated_by:
  name: si
  version: v0.2.0
resources:
  - key: service.name
    type: string
    binding:
      source: ir
      path: service.name
  - key: service.version
    type: string
    binding:
      source: runtime_resource
      path: runtime.resource.service.version
      fallback: unknown
spans:
  - id: tracing:ep:orders:root
    name: POST /orders/{id}
    kind: server
    target:
      type: endpoint
      id: ep:orders
    lifecycle:
      start: operation_start
      end: operation_end
    parent:
      strategy: extract_or_root
      carrier: http_headers
    attributes:
      - key: http.request.method
        type: string
        binding:
          source: constant
          string: POST
      - key: http.route
        type: string
        binding:
          source: ir
          path: endpoint.http_path
    status:
      mapping:
        cancelled: error
        error: error
        ok: unset
        timeout: error
        unknown: unset
    events:
      - id: tracing:ep:orders:root:timeout
        name: timeout
        statuses: [timeout]
      - id: tracing:ep:orders:root:cancelled
        name: cancelled
        statuses: [cancelled]
      - id: tracing:ep:orders:root:exception
        name: exception
        statuses: [error]
        attributes:
          - key: exception.type
            type: string
            binding:
              source: runtime_result
              path: runtime.error.type
```

## 2. 字段到 Runtime 的映射

| 计划字段（SpanPlan） | otel.yaml 字段 | Runtime 责任 |
| --- | --- | --- |
| `id` | `spans[].id` | 稳定标识，与 Plan 一致 |
| `name` | `spans[].name` | 已按 P1-04 规则生成，直接使用 |
| `kind` | `spans[].kind` | `server` / `client` / `producer` / `consumer` / `internal` |
| `target` | `spans[].target` | `endpoint` / `function` / `dependency` / `call_edge` |
| `start_trigger` / `end_trigger` | `spans[].lifecycle` | `operation_start` / `operation_end` / `state_change` |
| `parent.mode` | `spans[].parent.strategy` | `extract_or_root` / `new_root` / `current_context` / `static` |
| `parent.carrier` | `spans[].parent.carrier` | 仅 `extract_or_root`；`http_headers` / `grpc_metadata` / `kafka_headers` |
| `parent.static_parent_span_id` | `spans[].parent.static_parent_span_id` | 仅 `static`，必须引用同一文档的 span ID |
| `status` | `spans[].status.mapping` | 五个 runtime status 全覆盖，无 fallthrough |
| `events` | `spans[].events` | 受控事件名，绝不使用原始错误字符串 |
| `attributes`（service.name / service.version） | `resources` | 文档级分组，每个 span 不重复 |

## 3. Parent/Kind 组合规则

- Endpoint Root Span：`server` + `extract_or_root`（HTTP/gRPC）或 `internal` + `new_root`（Cron）。
- Dependency / CallEdge Span：必须 `client`/`producer`/`consumer`/`internal` + `current_context`；
  `new_root` 被拒绝（`GEN_RENDER_ERROR`）。
- `static` parent 引用不存在的 span ID 被拒绝。

## 4. 渲染规则

- Renderer 只从已验证 SpanPlan 复制字段；spans 按 Plan ID 排序，attributes 按 key，
  events 按 event ID 排序；输出 LF、UTF-8，无 anchor/alias/timestamp。
- Runtime binding 显式携带 source/path/type/required/fallback；不渲染伪运行时值。
- 不输出 disabled internal spans、空 attribute、空 event 或 unspecified enum。
- 渲染后立即用严格 Decoder 与语义 Validator 重新解析；失败返回 `GEN_RENDER_ERROR`
  并携带 span ID，不返回部分 bytes。
- Renderer 不访问文件系统、环境、时钟或网络；相同输入在任何目录、时区下字节一致。
- Semantic Conventions 版本固定为 `1.37.0`，不写 `latest` 或版本范围。
