# `metrics.yaml` 文件契约与渲染规则

本文档记录 `si generate` 输出的 `metrics.yaml`（P1-08）契约示例、字段到 Runtime
binding source 的映射和渲染规则。文件本身是封闭契约：`generator.metrics/v1` 发布后
字段集合固定；任何字段或 enum 变更都发布新的显式版本。

## 1. 渲染示例

以下示例是 `RenderMetricsPlan(plan, policy)` 的真实输出格式（Golden fixture
`testdata/golden/metrics_four_types.yaml`）：

```yaml
schema_version: generator.metrics/v1
document_type: instrumentation.metrics
source:
  ir_schema_version: v1
  service_name: orders
generated_by:
  name: si
  version: v0.2.0
metrics:
  - id: metrics:dep:sql:count
    name: orders_orders_store_exec_sql_operations_total
    type: counter
    unit: "{operation}"
    description: Number of SQL operations completed; unit {operation}; recorded at operation end
    target:
      type: dependency
      id: dep:sql
    record:
      trigger: operation_end
      value:
        source: constant
        number: 1
    attributes:
      - key: operation
        type: string
        binding:
          source: constant
          string: exec
      - key: service
        type: string
        binding:
          source: ir
          path: service.name
      - key: status
        type: string
        binding:
          source: runtime_result
          path: runtime.operation.status
          allowed_values: [ok, error, cancelled, timeout, unknown]
```

## 2. 字段到 Runtime binding source 的映射

| 计划字段（GenerationPlan.MetricPlan） | metrics.yaml 字段 | Runtime 责任 |
| --- | --- | --- |
| `id` | `metrics[].id` | 稳定标识，与 Plan 一致 |
| `name` | `metrics[].name` | 已归一化机器名，直接使用 |
| `type` | `metrics[].type` | `counter` / `histogram` / `gauge` / `summary` |
| `unit` | `metrics[].unit` | UCUM 兼容单位；空表示无量纲 |
| `description` | `metrics[].description` | 说明对象、单位和记录时机 |
| `target` | `metrics[].target` | `type` 为 `endpoint`/`function`/`dependency`/`call_edge` |
| `trigger.phase` | `metrics[].record.trigger` | `operation_start` / `operation_end` / `state_change` |
| `value` | `metrics[].record.value` | 见 §3 binding source 表 |
| histogram buckets（来自 Policy） | `metrics[].buckets` | 有限 bucket 边界，严格递增 |
| summary quantiles（来自 Policy） | `metrics[].quantiles` | 严格递增，位于 `[0, 1]` |
| `attributes[].key` | `metrics[].attributes[].key` | 只允许 `service`、`operation`、`status` |
| `function_id` | 不进入 YAML | 仅供 Dashboard/Runtime 引用 |

## 3. Binding source 映射

| 计划 source | metrics.yaml source | 值表达 | Runtime 责任 |
| --- | --- | --- | --- |
| `PLAN_CONSTANT` | `constant` | `plan.constant.one` → `number: 1`；`plan.constant.<text>` → `string: <text>` | 使用固定常量 |
| `IR_CONSTANT` | `ir` | `path` 逐字复制（如 `service.name`） | 从已验证 IR 字段读取 |
| `RUNTIME_RESULT` | `runtime_result` | `path` 逐字复制（如 `runtime.operation.duration_seconds`） | 操作结果；`required: true` 时缺失必须按 fallback 或失败处理 |
| `RUNTIME_RESOURCE` | `runtime_resource` | `path` 逐字复制 | 运行时资源信息 |
| `RUNTIME_CONTEXT` | `runtime_context` | `path` 逐字复制 | 请求上下文 |
| `VALUE_TYPE_STATUS` | `type: status` + `allowed_values` | 固定五个枚举 | 缺失时使用 fallback，绝不填充伪值 |

## 4. 渲染规则

- Renderer 只从已验证 MetricPlan 复制字段，不重新推导名称、属性或安全决策。
- definitions 按 Plan ID 排序，attributes 按 key 排序；输出 LF、UTF-8，无 anchor/alias/timestamp。
- 浮点数采用稳定、可往返的格式；histogram buckets 与 summary quantiles 来自 Policy。
- 渲染后立即用严格 Decoder 与语义 Validator 重新解析；失败返回 `GEN_RENDER_ERROR`，
  不返回部分 bytes。
- Renderer 不访问文件系统、环境、时钟或网络；相同输入在任何目录下字节一致。
- Plan 与 Policy 的不一致（例如 Policy 启用 Summary 但 Plan 未包含）由
  `CheckPlanPolicyConsistency` 报告，Renderer 按 Plan 渲染，不自行补全。
