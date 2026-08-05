# Root Span Plan 契约（P1-09）

本文档记录 Endpoint Root Span 的映射、OpenTelemetry Semantic Conventions 版本、
属性词汇与 parent/carrier 策略。`generator/planner/tracing` 包实现该映射；同一份 IR
在任何环境下规划结果字节一致。

## 1. Semantic Conventions 版本

Root Span 的所有属性映射固定使用 OpenTelemetry Semantic Conventions **`1.37.0`**。
不写 `latest`、版本范围或运行时查询结果；版本由内置常量固定，不可配置。

## 2. Root Span 映射

| Endpoint | Span Kind | Name | Parent Strategy | Carrier |
| --- | --- | --- | --- | --- |
| HTTP | `SERVER` | `<METHOD> <route-template>`（未知 method 用 `HTTP`） | `extract_or_root` | `http_headers` |
| gRPC | `SERVER` | `<service>/<method>` | `extract_or_root` | `grpc_metadata` |
| Cron | `INTERNAL` | `cron <stable-job-name>` | `new_root` | `none` |

- 每个 Endpoint 最多生成一个 Root Span；Span ID 由 Endpoint ID 与 `root` purpose
  稳定派生（`tracing:<endpoint-id>:root`），名称变化不改变 ID。
- lifecycle 绑定 handler/server method/job callback 的 entry/exit
  （`operation_start` / `operation_end`）；duration 由 Runtime 在两个 trigger 之间采集。
- `extract_or_root` 表示从受支持 carrier 提取远程 parent，缺失时创建 root；
  `new_root` 始终创建新 root，除非 Runtime 显式提供 context。
- carrier 是抽象标识（HTTP headers / gRPC metadata 类），Plan 不读取任何真实
  carrier 或请求；Runtime 负责实际提取。

## 3. 属性词汇（semconv 1.37.0 + 统一词汇）

| 逻辑词汇 | 标准属性 | 来源 | 说明 |
| --- | --- | --- | --- |
| service | `service.name` | IR 常量 | 每个 Plan 中常量 |
| version | `service.version` | runtime resource | 缺失时 fallback `unknown` |
| module | `code.namespace` | IR 常量 `function.package_path` | 缺失时省略 |
| function | `code.function` | IR 常量 `function.qualified_name` | 缺失时省略 |
| HTTP method | `http.request.method` | Plan 常量 | 未知 method 用受控 `HTTP` |
| HTTP route | `http.route` | IR 常量 `endpoint.http_path` | 静态模板 |
| gRPC system | `rpc.system` | Plan 常量 `grpc` | |
| gRPC service | `rpc.service` | IR 常量 `endpoint.grpc_service` | |
| gRPC method | `rpc.method` | IR 常量 `endpoint.grpc_method` | |
| Cron job | `cron.job.name` | IR 常量 `endpoint.name` | 稳定 job 标识 |
| Cron schedule | `cron.job.schedule` | IR 常量 `endpoint.cron_schedule` | 静态 schedule，缺失省略 |

- 统一词汇映射为标准属性，不输出重复 alias（同一 Plan 中 `service.name` /
  `service.version` 各出现一次）。
- 禁止属性：query、headers、body、raw URL、userinfo、fragment、payload、
  request/response 内容。
- Dependency 的 `target_url`、`target_service`、`resource`、`value` 不复制到任何
  Root Span 属性；Endpoint route 只作为受控静态模板。

## 4. 降级与失败语义

- HTTP 缺 method/route、gRPC 缺 service/method、Cron 缺 job 名：非 strict 模式使用
  Function identity（`function.qualified_name`）作为 span 名称并产生
  `GEN_INCOMPLETE_TARGET`；绝不使用 source path 作为名称。strict 模式失败。
- 未知 EndpointKind：`GEN_UNSUPPORTED_ENTITY` 并跳过，禁止猜测。
- 普通 Function 或 Dependency 不创建 Root；Kafka Consumer 不推断 Handler Root
  （留给 Dependency Span 规则 P1-10）。
- status 映射固定：`ok → unset`、`error/timeout/cancelled → error`、
  `unknown → unset`；无 default fallthrough。错误事件由 P1-10 添加。
