# `logging.yaml` 文件契约与渲染规则

本文档记录 `si generate` 输出的 `logging.yaml`（P1-13）契约示例、字段到 Runtime
binding source 的映射和渲染规则。文件是封闭契约：`generator.logging/v1` 发布后字段
集合固定。

## 1. 渲染示例

以下示例是 `RenderLoggingPlan(plan, policy)` 的真实输出格式（Golden fixture
`testdata/golden/logging_minimal.yaml`）：

```yaml
schema_version: generator.logging/v1
document_type: instrumentation.logging
source:
  ir_schema_version: v1
  service_name: orders
generated_by:
  name: si
  version: v0.2.0
redaction:
  immutable: true
  field_names:
    - authorization
    - cookie
    - password
    - secret
    - token
events:
  - id: logging:ep:orders:completed
    event_name: http.request.completed
    target:
      type: endpoint
      id: ep:orders
    trigger: operation_end
    condition:
      status_in: [ok]
    severity:
      constant: info
    fields:
      - key: timestamp
        type: timestamp
        binding:
          source: runtime_clock
          path: runtime.clock
      - key: event.name
        type: string
        binding:
          source: constant
          string: http.request.completed
      - key: service
        type: string
        binding:
          source: ir
          path: service.name
      - key: version
        type: string
        binding:
          source: runtime_resource
          path: runtime.resource.service.version
          fallback: unknown
      - key: status
        type: status
        binding:
          source: runtime_result
          path: runtime.operation.status
      - key: duration_seconds
        type: double
        binding:
          source: runtime_result
          path: runtime.operation.duration_seconds
      - key: trace_id
        type: string
        required: true
        binding:
          source: runtime_context
          path: runtime.context.trace_id
```

## 2. 字段到 Runtime binding source 的映射

| 字段 | binding source | Runtime 责任 |
| --- | --- | --- |
| `timestamp` | `runtime_clock` | 发射时刻写入；Generator 从不填具体时间 |
| `event.name` | `constant` | 固定事件名，如 `http.request.completed` |
| `service` / `module` / `function` | `ir` | 从已验证 IR 字段读取 |
| `operation` | `constant` | 受控归一化操作 |
| `version` | `runtime_resource` | 缺失时使用 fallback `unknown` |
| `status` / `duration_seconds` | `runtime_result` | 仅出现在 end 事件 |
| `error.type` / `error.category` | `runtime_result` | 仅 failed 事件；message/stacktrace 永不绑定 |
| `request_id` | `runtime_context` | 可选；缺失时省略 |
| `trace_id` / `span_id` | `runtime_context` | Endpoint 事件 required；Dependency 事件 optional |

## 3. Condition 与 Severity

- `condition.status_in` 按有限 status 顺序输出：`ok, error, cancelled, timeout, unknown`。
- completed 事件只匹配 `ok`；failed 事件匹配 `error/timeout/cancelled/unknown`；
  同一 target 的 completed/failed 互斥，重叠即渲染失败（`GEN_RENDER_ERROR`）。
- 无条件事件（如 start）展开为全部五个 status。
- Severity：`ok → info`、`cancelled/unknown → warn`、`timeout/error → error`。

## 4. 脱敏与安全

- 顶层 `redaction.immutable: true`：内置 credential denylist 不可关闭；用户规则只能追加。
- 所有字段 key 通过统一 policy（大小写无关、`-`/`_` 等价）；规范化后重复即失败。
- 渲染后复验：strict decoder、semantic validator 和敏感常量扫描；canary 不进入文件或错误消息。

## 5. Optional 字段省略语义

- optional binding 缺失时 Runtime **省略字段**，绝不输出空字符串占位、`unknown` 或随机值。
- `request_id` 永远 optional；`trace_id`/`span_id` 在无 Root Span context 的
  Dependency 事件上为 optional（P1-12 AC4）。

## 6. 渲染规则

- Events 按 Plan ID 排序，fields 按 key 排序；输出 LF、UTF-8、无 BOM，无 anchor/alias/timestamp。
- Renderer 不自行增加 error.message、payload、headers、SQL 或 key 字段。
- 渲染失败返回 `GEN_RENDER_ERROR` 并携带 LogPlan ID，不返回部分 bytes。
- Renderer 不访问文件系统、环境、时钟或网络；相同输入在任何目录、时区下字节一致。
