# Phase 1 - Observability Generator Jira Stories

## 文档信息

| 字段 | 内容 |
| --- | --- |
| 状态 | Draft |
| 目标版本 | `v0.2.0` |
| 计划周期 | 4 周 |
| 更新日期 | 2026-08-03 |
| 上游基线 | Phase 0 Compiler Foundation 已完成并通过发布检查 |
| 目标读者 | Product Owner、Tech Lead、Go 开发人员、QA、SRE |

## 1. Phase 目标

Phase 1 将 Phase 0 产出的 `ObservabilityDocument` 转换为稳定、可校验、可由后续运行时或代码生成器消费的可观测性声明文件：

```text
Source Code
    -> Language Analyzer
    -> Observability IR
    -> Generation Planner
    -> Signal Renderers
    -> generate/metrics.yaml
    -> generate/otel.yaml
    -> generate/logging.yaml
```

用户通过以下命令完成扫描和生成：

```sh
si generate [path]
```

### 1.1 用户价值

- 将端点、依赖和调用关系转换为统一的 Metrics、Tracing、Logging 规范，减少人工设计成本。
- 对同一份 IR 生成确定且可审查的配置，便于纳入 Git 和 CI。
- 通过统一命名、低基数属性和敏感数据保护策略，提前阻止高成本或不安全的观测配置。
- 为 Phase 2 Dashboard Generator 提供稳定的指标和 Span 标识，而不是重新分析源码。

### 1.2 Phase 成功标准

1. 对有效 Phase 0 IR 执行 `si generate` 后，默认生成 `metrics.yaml`、`otel.yaml`、`logging.yaml` 三个文件。
2. 三个文件均符合版本化 Schema，能够被严格解析和语义校验。
3. 同一源码、配置、CLI 版本重复生成时，三个文件的字节内容完全一致。
4. Generator 仅依赖 IR，不导入 `compiler/goanalyzer`、`go/ast`、`go/types` 或 `go/packages`。
5. Phase 0 已支持的每种 Endpoint 和 Dependency 要么生成明确定义的观测计划，要么产生稳定 Diagnostic，禁止静默遗漏。
6. 默认输出不把原始 URL、SQL、Redis Key、消息内容、动态 Topic、凭据或 PII 放入指标标签或日志字段。
7. 生成失败时不留下截断文件；未传 `--force` 时不覆盖已有目标文件。
8. `si scan` 的现有参数、JSON 契约、退出码和离线行为不回归。

### 1.3 产品结果指标

| 指标 | Phase 1 目标 |
| --- | --- |
| Phase 0 实体处理覆盖率 | 100%，生成或给出 Diagnostic |
| Golden fixture 确定性 | 连续 10 次执行无字节差异 |
| 配置 Schema 校验通过率 | 所有内置 fixture 为 100% |
| 高基数危险字段默认进入 Metrics | 0 |
| 明文敏感测试值进入生成文件 | 0 |
| 生成过程外部网络请求 | 0 |

## 2. 范围定义

### 2.1 In Scope

- 在 Protobuf IR 中增加语言无关、向后兼容的 Generation Plan。
- 建立 Generator Policy、Planner、Renderer 和 Validator 分层。
- 为 HTTP、gRPC、Cron、Kafka、SQL、Redis、HTTP Client、RPC Client 生成观测计划。
- 支持 Counter、Histogram、Gauge、Summary 类型；Summary 默认关闭，可通过策略显式启用。
- 生成 Root Span、Dependency Span、Attributes、Status 和 Error Events。
- 生成结构化日志事件、Trace 关联字段和默认脱敏规则。
- 新增离线 `si generate` 命令、配置、原子写入保护、`--dry-run`、`--force` 和 `--strict`。
- 增加 Schema、单元测试、Golden fixture、端到端测试、性能基线和发布检查。

### 2.2 Out of Scope

- 修改用户源码、自动插桩、编译期代码注入或运行时 SDK 实现。
- 启动或部署 OpenTelemetry Collector、Prometheus、Grafana、Loki、Datadog 或 New Relic。
- 证明生成的观测数据已经在运行时真实产生。
- Dashboard、SLO、Alert、Runbook、CI Linter 和 IDE 功能。
- Tail Sampling、Collector Pipeline、Exporter 凭据和后端连接配置。
- 从运行时请求、响应、SQL 参数、消息 Body 中提取值。
- 为 Phase 0 尚未识别的框架增加新的 AST 识别规则。
- Kafka Handler Root Span 推断。当前 IR 只证明 Consumer API 调用，不能可靠证明消息处理回调；本 Phase 仅生成调用点计划并给出能力说明。

> **边界说明：** Phase 1 输出的是版本化的 Instrumentation Plan，不是“已经运行的遥测”。后续 Runtime 或代码生成层负责把 `runtime.*` 值绑定到具体 SDK。

## 3. 架构与契约决策

### 3.1 固定分层

| 层 | 输入 | 输出 | 禁止事项 |
| --- | --- | --- | --- |
| Analyzer | Source Code | `ObservabilityDocument` | 不生成 YAML |
| Planner | IR + Policy | `GenerationPlan` | 不读取源码或 AST |
| Renderer | `GenerationPlan` | 内存中的 YAML bytes | 不写文件、不补业务推断 |
| Validator | YAML model | Validation Result | 不自动修复无效值 |
| CLI Writer | 已验证 bytes | 目标文件 | 不修改计划内容 |

固定调用链：

```text
Analyze -> Validate IR -> Plan -> Validate Plan -> Render In Memory
        -> Validate Rendered Documents -> Commit Files
```

任一阶段出现 fatal error 时不得继续到下一阶段。

### 3.2 Generation Plan 所有权

- 在 `ir/v1/generation.proto` 定义 `GenerationPlan` 及其强类型子消息。
- 在 `ObservabilityDocument` 末尾追加可选 `generation_plan` 字段，不修改任何已发布 field number。
- Analyzer 不负责填充该字段；Planner 接收分析 IR 并返回独立 Plan。
- 所有 Plan 项使用强类型 `target` 引用 IR 中的 Endpoint、Function、Dependency 或 CallEdge 稳定 ID；Internal Span 必须引用具体 CallEdge，不能只引用 Function 而丢失调用点。
- `scan` 默认仍输出不含 Plan 的分析结果；`generate` 可以在内存中组合二者。
- IR Schema Version 和 Generator Schema Version 独立演进，输出文件必须同时记录二者。

### 3.3 输出契约

默认输出目录为 `<source-root>/generate`，包含且仅管理以下文件：

| 文件 | `document_type` | 内容 |
| --- | --- | --- |
| `metrics.yaml` | `instrumentation.metrics` | 指标定义、记录时机、值来源、属性绑定 |
| `otel.yaml` | `instrumentation.tracing` | Span 定义、Kind、上下文、属性、Status、Events |
| `logging.yaml` | `instrumentation.logging` | 结构化事件、级别、字段来源、关联和脱敏策略 |

每个文件必须包含：

- `schema_version`
- `document_type`
- `source.ir_schema_version`
- `source.service_name`
- `generated_by.name`
- `generated_by.version`
- 对应 signal 的有序定义列表

禁止包含生成时间、绝对路径、随机 UUID、主机名或用户名。

### 3.4 默认信号矩阵

| IR 实体 | Metrics | Tracing | Logging |
| --- | --- | --- | --- |
| HTTP Handler | 请求数、耗时、并发请求数 | `SERVER` Root Span | 完成、失败事件 |
| gRPC Handler | 请求数、耗时、并发请求数 | `SERVER` Root Span | 完成、失败事件 |
| Cron Job | 执行数、耗时、运行中任务数 | `INTERNAL` Root Span | 完成、失败事件 |
| Kafka Producer | 操作数、耗时；失败数由 `status` 分区 Counter 派生 | `PRODUCER` Child Span | 默认仅失败事件 |
| Kafka Consumer API | 操作数、耗时；失败数由 `status` 分区 Counter 派生 | `CONSUMER` 调用点 Span，不推断 Handler Root | 默认仅失败事件 |
| SQL | 操作数、耗时；失败数由 `status` 分区 Counter 派生 | `CLIENT` Child Span | 默认仅失败事件 |
| Redis | 操作数、耗时；失败数由 `status` 分区 Counter 派生 | `CLIENT` Child Span | 默认仅失败事件 |
| HTTP Client | 操作数、耗时；失败数由 `status` 分区 Counter 派生 | `CLIENT` Child Span | 默认仅失败事件 |
| RPC Client | 操作数、耗时；失败数由 `status` 分区 Counter 派生 | `CLIENT` Child Span | 默认仅失败事件 |
| Resolved Internal Call | 默认不生成 | 策略开启后生成 `INTERNAL` Child Span | 默认不生成 |

### 3.5 统一属性词汇

Phase 1 路线图中的统一维度解释为受控 Attribute Vocabulary。每个定义只引用适用的维度，不得输出值为空的标签。

| 属性 | 来源 | Metrics 默认行为 | 说明 |
| --- | --- | --- | --- |
| `service` | `service.name` | 默认 Attribute，单个 Plan 中为常量 | 服务名 |
| `module` | `function.package_path` 的稳定归一值 | 不作为默认 Attribute；进入稳定名称和 target metadata | 模块或包 |
| `function` | `function.qualified_name` | 不作为默认 Attribute；进入稳定名称和 target metadata | 函数名 |
| `operation` | Endpoint/Dependency operation | 默认 Attribute，单个 target 中为常量 | 操作类型 |
| `status` | `runtime.operation.status` | 默认 Attribute，仅允许 5 个枚举值 | `ok`、`error`、`cancelled`、`timeout`、`unknown` |
| `version` | `runtime.resource.service.version` | 不作为默认 Metric Attribute | 用于 Trace Resource 和 Log；缺失为 `unknown` |

Metrics 默认 Attribute 仅为 `service`、`operation`、`status`；Gauge 不带 `status`。`histogram_buckets_seconds` 的每个值表示一个有限 bucket boundary。Planner 使用保守的 classic-exposition 估算：Counter/Gauge 每个属性组合计 1 条，Histogram 每个组合计 `finite_boundary_count + 3` 条（有限 buckets、隐式 `+Inf`、sum、count），Summary 每个组合计 `quantile_count + 2` 条（quantiles、sum、count）。后端采用 native histogram 时实际 series 可以更少，但不得降低规划预算。预计 instrument 数或 series 数超过 Policy 上限时返回 `GEN_CARDINALITY_LIMIT_EXCEEDED`，不得静默截断。

### 3.6 默认安全与基数策略

以下内容默认不得成为 Metric Attribute 或普通日志字段：

- 原始 HTTP URL、Query String、Header、Cookie、Request/Response Body。
- 原始 SQL 文本、SQL 参数、Redis Key/Value。
- Kafka Message Key/Body、动态 Topic、Consumer Payload。
- Access Token、Authorization、API Key、Password、Secret、Session ID。
- Email、手机号、身份证号等 PII。
- Source Root 绝对路径和本机环境变量值。

Tracing 只允许输出 Endpoint route、RPC endpoint service/method、数据库 system/operation、消息系统名称等低风险属性。Phase 1 不把任何 Dependency 的 `target_url`、`target_service`、`resource` 或 `value` 复制到 Metric、Span 或 Log；现有 `value_is_static` 只证明 `Dependency.value`，不得被解释为其他字段的 provenance。被策略阻止的字段产生 Diagnostic；不得静默复制。

## 4. Generator 配置草案

Phase 1 在现有 `si.yaml` 中增加可选 `generation` 节点：

```yaml
generation:
  output_dir: generate
  signals: [metrics, tracing, logging]
  strict: false

  metrics:
    namespace: ""
    histogram_buckets_seconds: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
    include_in_flight_gauges: true
    max_instruments: 10000
    max_estimated_series: 100000
    summaries:
      enabled: false
      quantiles: [0.5, 0.9, 0.99]

  tracing:
    include_internal_calls: false
    record_exception_events: true

  logging:
    emit_start_events: false
    emit_completion_events: true
    emit_dependency_errors: true
    correlation_fields: [request_id, trace_id, span_id]
    redact_fields: [authorization, cookie, password, secret, token]
```

配置优先级固定为：

```text
CLI flags > si.yaml > built-in defaults
```

不得读取环境变量来隐式改变生成内容；环境变量只能由未来 Runtime 在执行时提供 `runtime.*` 值。

## 5. Diagnostic 和失败语义

### 5.1 Generator Diagnostic Codes

| Code | 默认级别 | 含义 | `--strict` 行为 |
| --- | --- | --- | --- |
| `GEN_UNSUPPORTED_SCHEMA` | Error | 输入 IR 版本不受支持 | 失败 |
| `GEN_INVALID_IR` | Error | 缺少 Service、ID 重复或结构无效 | 失败 |
| `GEN_DANGLING_REFERENCE` | Error | Plan 所需引用不存在 | 失败 |
| `GEN_INVALID_CONFIG` | Error | 配置类型、范围或组合无效 | 失败 |
| `GEN_NAME_COLLISION` | Warning | 归一化后名称冲突并已稳定消歧 | 失败 |
| `GEN_CARDINALITY_BLOCKED` | Warning | 高基数属性被移除 | 失败 |
| `GEN_CARDINALITY_LIMIT_EXCEEDED` | Error | 预计 instrument 或 series 超过 Policy 上限 | 失败 |
| `GEN_SENSITIVE_VALUE_DROPPED` | Warning | 潜在敏感值被移除 | 失败 |
| `GEN_UNSUPPORTED_ENTITY` | Warning | 实体无法安全映射 | 失败 |
| `GEN_INCOMPLETE_TARGET` | Warning | 动态或未知目标导致部分属性缺失 | 失败 |
| `GEN_RENDER_ERROR` | Error | Plan 无法渲染为合法文档 | 失败 |
| `GEN_OUTPUT_EXISTS` | Error | 未指定 `--force` 且目标已存在 | 失败 |

### 5.2 CLI 退出码

- `0`：生成成功；非 strict 模式允许 Warning。
- `1`：扫描失败、IR/Plan/Render 失败、strict Warning、文件写入失败。
- `2`：参数、路径或配置错误。

沿用 Phase 0 的 `CLI_INVALID_ARGUMENT`、`CLI_SCAN_ERROR`、`CLI_INTERNAL_ERROR`，并新增 `CLI_GENERATE_ERROR` 表示 Plan、Render、Validate 或 Commit 失败。`generate --format json` 在 flags 已成功解析后，无论成功、Warning、配置错误或 pipeline 错误，都向 stdout 写入且仅写入一个版本化 `GenerateReport`；stderr 保持为空，外层 runner 不重复打印已报告错误。Cobra 在识别 `--format` 前发生的 flag 语法错误仍使用 Phase 0 文本 stderr。文本模式下 Generator Diagnostic 进入 stderr 摘要。

## 6. Epic、Story 和依赖

### 6.1 Epic 列表

| Epic | 目标 |
| --- | --- |
| `P1-CONTRACT` | 建立 Plan、Schema、Policy、命名与 Planner 基础 |
| `P1-METRICS` | 生成并校验 `metrics.yaml` |
| `P1-TRACING` | 生成并校验 `otel.yaml` |
| `P1-LOGGING` | 生成并校验 `logging.yaml` |
| `P1-CLI` | 提供安全、离线、确定的 `si generate` 工作流 |
| `P1-QUALITY` | 建立 Fixture、质量门禁和发布标准 |

### 6.2 Story 总览

| Story | 标题 | Epic | 优先级 | SP | 依赖 |
| --- | --- | --- | --- | ---: | --- |
| P1-01 | 定义版本化 Generation Plan Protobuf | P1-CONTRACT | Highest | 5 | Phase 0 DoD |
| P1-02 | 定义三类 YAML 契约与严格校验器 | P1-CONTRACT | Highest | 3 | P1-01 |
| P1-03 | 实现 Generator Policy 配置与合并规则 | P1-CONTRACT | Highest | 3 | P1-01 |
| P1-04 | 实现统一命名、属性基数与隐私策略 | P1-CONTRACT | Highest | 3 | P1-01 |
| P1-05 | 建立确定性 Planner 与 IR 校验 | P1-CONTRACT | Highest | 5 | P1-01, P1-03, P1-04 |
| P1-06 | 生成 Endpoint Metrics Plan | P1-METRICS | High | 3 | P1-05 |
| P1-07 | 生成 Dependency Metrics Plan 和全部指标类型 | P1-METRICS | High | 5 | P1-05 |
| P1-08 | 渲染并校验 `metrics.yaml` | P1-METRICS | High | 3 | P1-02, P1-06, P1-07 |
| P1-09 | 生成 Endpoint Root Span Plan | P1-TRACING | High | 3 | P1-05 |
| P1-10 | 生成 Child Span、Attributes、Status 和 Events | P1-TRACING | High | 5 | P1-09 |
| P1-11 | 渲染并校验 `otel.yaml` | P1-TRACING | High | 3 | P1-02, P1-10 |
| P1-12 | 生成结构化 Logging Plan 和关联脱敏策略 | P1-LOGGING | High | 5 | P1-05 |
| P1-13 | 渲染并校验 `logging.yaml` | P1-LOGGING | High | 3 | P1-02, P1-12 |
| P1-14 | 实现 `si generate` 和安全文件提交 | P1-CLI | Highest | 5 | P1-08, P1-11, P1-13 |
| P1-15 | 建立 Generator Fixture 与端到端 Golden 测试 | P1-QUALITY | Highest | 5 | P1-14 |
| P1-16 | 建立质量门禁、性能基线和发布检查 | P1-QUALITY | High | 3 | P1-15 |

总建议工作量为 **62 SP**。该估算适用于能够并行开发 Metrics、Tracing、Logging 的团队；进入 Sprint 前必须按团队最近三次迭代速度重新校准，不把 SP 换算为固定工时。

### 6.3 依赖图

```mermaid
flowchart LR
    P101[P1-01 Plan Proto] --> P102[P1-02 YAML Contracts]
    P101 --> P103[P1-03 Policy]
   P101 --> P104[P1-04 Naming and Safety]
   P103 --> P105[P1-05 Planner]
    P104 --> P105[P1-05 Planner]
    P105 --> P106[P1-06 Endpoint Metrics]
   P105 --> P107[P1-07 Dependency Metrics]
    P102 --> P108[P1-08 Metrics Renderer]
   P106 --> P108
    P107 --> P108
    P105 --> P109[P1-09 Root Spans]
    P109 --> P110[P1-10 Child Spans]
    P102 --> P111[P1-11 OTel Renderer]
    P110 --> P111
    P105 --> P112[P1-12 Logging Plan]
    P102 --> P113[P1-13 Logging Renderer]
    P112 --> P113
    P108 --> P114[P1-14 CLI]
    P111 --> P114
    P113 --> P114
    P114 --> P115[P1-15 E2E Fixtures]
    P115 --> P116[P1-16 Release Gates]
```

### 6.4 四周条件排期

四周是**有条件目标**，要求团队最近四周可交付不少于 62 SP，并至少有 4 名可并行开发人员，以及可参与契约、安全和验收的 Tech Lead、SRE/QA。若不满足该容量，应将计划调整为 6 至 8 周，不得通过删除 P1-15/P1-16 或降低安全验收来维持日期。第 6.2 节依赖是 Story 完成门禁；测试骨架、Schema 草案和 CI scaffolding 可在上游接口冻结后提前开展，但不得让下游 Story 在依赖未完成时进入 Done。

| 周次 | 主要目标 | 可并行工作 |
| --- | --- | --- |
| Week 1 | P1-01；随后 P1-02、P1-03、P1-04 并行；启动 P1-05 | 冻结 Proto/YAML/Policy/命名接口；同时搭建 fixture、Schema 和 CI 骨架 |
| Week 2 | 完成 P1-05；P1-06、P1-07、P1-09、P1-12 并行；P1-09 完成后立即开始 P1-10 | Metrics、Logging 与 Root Span 并行；Tracing 在 P1-09 到 P1-10 间顺序交接；持续补充 P1-15 fixture |
| Week 3 | P1-08、P1-11、P1-13 并行；完成后接入 P1-14 | Renderer contract tests 与 CLI Writer fault tests 同步开发 |
| Week 4 | 完成 P1-14、P1-15、P1-16 | 跨平台、确定性、安全、性能和 RC 发布演练；不再接受契约级范围变更 |

## 7. 统一 Jira 标准

### 7.1 Definition of Ready

Story 进入开发前必须满足：

- 用户价值、In Scope、Out of Scope 和依赖已明确。
- 所有外部契约字段、默认值和失败语义已由 Tech Lead 与下游消费者确认。
- 验收标准可独立测试，不使用“正常工作”“尽量”“适当”等不可验证表述。
- 所需 Fixture、测试数据和上游接口已经存在，或明确属于当前 Story。
- Story 不跨越两个不可独立发布的架构层；超过 8 SP 必须继续拆分。
- 没有未决安全问题、Schema 兼容问题或需要在线服务才能验证的问题。

### 7.2 全局 Definition of Done

每个 Story 除自身 DoD 外，还必须满足：

- 生产代码、测试、Schema 和文档在同一个 Pull Request 中完成。
- 所有新增公开 Go 类型、函数和 Protobuf 字段有用途说明。
- 不使用 `any`、未约束 `map[string]interface{}` 或吞掉错误的空 `catch`/错误分支。
- 错误附带阶段和实体 ID 上下文；面向用户的错误有稳定 message code。
- 新增逻辑包含 Happy Path、边界条件和 Error Path 测试。
- 测试离线、无时钟依赖、无随机性、无机器绝对路径。
- `go test`、`go vet`、race test、Schema 检查和相关 Golden tests 通过。
- 同一输入重复运行输出一致，生成文件无未解释差异。
- 不导入 Analyzer/AST 包，不引入共享可变全局状态。
- PR 描述包含验收证据、测试命令、兼容性影响和已知限制。

---

## P1-01 定义版本化 Generation Plan Protobuf

**Issue Type：** Story  
**Epic：** P1-CONTRACT  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** Phase 0 Definition of Done  
**阻塞：** P1-02、P1-03、P1-04、P1-05  
**Labels：** `phase-1`、`ir`、`protobuf`、`backward-compatible`

### 用户故事

作为 Generator 和未来 Runtime 的开发者，我需要一个语言无关、强类型、可演进的 Generation Plan，使 Metrics、Tracing、Logging Renderer 不依赖 Go AST 或各自重复推断。

### 业务价值

- 将观测设计决策保存在统一契约中，为 Dashboard、Runtime 和 AI 提供可复用输入。
- 让三类 Renderer 共享目标引用、属性来源和 Diagnostic 语义。
- 通过 Protobuf append-only 规则降低后续多语言和多后端扩展成本。

### 背景与问题

当前 `ObservabilityDocument` 只描述代码事实，不包含 Metric、Span 或 Log 定义。如果三个 Renderer 直接从 Endpoint/Dependency 各自推断，将产生不一致命名、重复安全逻辑和无法统一测试的问题。

### In Scope

- 新增 `ir/v1/generation.proto`。
- 定义 Plan、Metric、Span、Log、Attribute Binding、Trigger、Status Policy 和 Event 等消息及 enum。
- 在 `ObservabilityDocument` 追加可选 Plan 字段。
- 实现 Protobuf 二进制和 JSON 往返测试、引用校验入口及生成一致性检查。

### Out of Scope

- 具体 Endpoint/Dependency 到信号的映射。
- YAML 字段布局。
- 修改任何已发布字段编号或删除 enum 值。
- 在 Analyzer 中构造 Generation Plan。

### 输入与输出

- 输入：当前 `ir/v1/observability.proto` 及其兼容性规则。
- 输出：`GenerationPlan` Protobuf 类型和生成的 Go 代码。

### 实现任务

1. 定义 `GenerationPlan`，至少包含：
   - `schema_version`
   - `source_ir_schema_version`
   - `service_name`
   - 有序的 `metrics`、`spans`、`logs`、`diagnostics`
2. 定义 `MetricPlan`：稳定 ID、name、type、unit、description、target ID/type、record trigger、value source、attribute bindings。
3. 定义指标类型 enum：`COUNTER`、`HISTOGRAM`、`GAUGE`、`SUMMARY`；保留 `UNSPECIFIED = 0`。
4. 定义 `SpanPlan`：稳定 ID、name、kind、target ID/type、start/end trigger、parent strategy、attributes、status mapping、events。
5. 定义 Span Kind：`SERVER`、`CLIENT`、`PRODUCER`、`CONSUMER`、`INTERNAL`；命名与 OpenTelemetry 对齐但不直接暴露 SDK 类型。
6. 定义 `LogPlan`：稳定 ID、event name、severity、target ID/type、trigger、field bindings、correlation fields、redaction policy ID。
7. 定义强类型 `TargetRef`，至少支持 `ENDPOINT`、`FUNCTION`、`DEPENDENCY`、`CALL_EDGE`；target kind 与 ID 指向的 IR 实体类型不一致时校验失败。
8. 定义通用 `ValueBinding`，明确区分：
   - Plan 常量，例如 Counter 的 `1` 或由已验证 IR 字段规范化得到的 operation
   - IR 常量，如 `service.name`、`endpoint.http_path`
   - Runtime Resource，如 `runtime.resource.service.version`
   - Runtime Context，如 `runtime.context.trace_id`
   - Runtime Result，如 `runtime.operation.status`、`runtime.operation.duration`
9. 为 Binding 定义数据类型、required、fallback 和 cardinality class；不得使用任意 JSON map 代替强类型字段。
10. 在 `ObservabilityDocument` 使用新的、未占用 field number 追加 `generation_plan`，并在 proto 中注明 Analyzer 不填充该字段。
11. 更新生成任务和 `make check-generated`，确保两个 proto 文件按固定工具版本生成。
12. 建立 Plan 基础校验函数，检查 schema version、空 ID、重复 ID、空 target ID、target kind/ID 类型不匹配和 unspecified enum。
13. 增加兼容性测试：旧 JSON/二进制 IR 可由新类型读取；未含 Plan 时行为不变。
14. 增加确定性序列化测试，禁止 Plan 包含 timestamp、随机 ID 或绝对路径字段。
15. 在 `ir/doc.go` 或等价文档中说明 Analysis Facts 与 Generation Plan 的所有权边界。

### 验收标准

#### AC1：旧 IR 向后兼容

- **Given** 一个由 Phase 0 Schema 生成且不含 `generation_plan` 的二进制和 JSON fixture
- **When** 使用 Phase 1 类型反序列化并再次序列化
- **Then** 原有字段值保持不变，Plan 为 absent，且不产生错误

#### AC2：Plan 完整往返

- **Given** 一个同时包含 Counter、Histogram、Gauge、Summary、五种 Span Kind 和 Log Plan 的文档
- **When** 分别执行确定性 Protobuf 与 JSON 往返
- **Then** 输入输出语义相等，稳定 ID、Binding 和 enum 无丢失

#### AC3：非法 Plan 被拒绝

- **Given** Plan 中存在重复 ID、空 target ID 或 unspecified type
- **When** 调用 Plan Validator
- **Then** 返回带具体字段路径和实体 ID 的错误，不 panic，不返回部分有效结果

#### AC4：Analyzer 边界不变

- **Given** 任意 Phase 0 Analyzer 测试
- **When** 重新生成 Protobuf 并执行测试
- **Then** Analyzer 不需要构造 Plan，`si scan --format json` 契约不新增非空生成内容

#### AC5：生成代码无漂移

- **Given** 干净工作区和固定版本 Protobuf 工具
- **When** 连续执行两次生成任务
- **Then** 第二次执行不产生 Git diff

### 测试要求

- Table-driven unit tests：每种 enum、每种 Binding 来源、空 Plan、完整 Plan、重复 ID、悬空 target。
- Compatibility tests：提交 Phase 0 binary fixture 和 JSON fixture。
- Determinism tests：同一 Plan 序列化 10 次并比较 bytes。
- Negative compile boundary check：Generator IR 包不得导入 Analyzer 或 AST 包。

### 非功能要求

- 校验复杂度为 $O(M + S + L)$，其中 $M$、$S$、$L$ 分别是 Metric、Span、Log 数量。
- Validator 不修改输入消息。
- 所有 slice 返回值需防止调用方意外共享可变状态。

### Story DoD

- 所有 AC 有自动化测试。
- Proto 注释、生成代码、兼容 fixture 和 IR 文档已提交。
- `make generate` 与 `make check-generated` 通过。
- Tech Lead 完成 Schema compatibility review。

---

## P1-02 定义三类 YAML 契约与严格校验器

**Issue Type：** Story  
**Epic：** P1-CONTRACT  
**优先级：** Highest  
**建议工作量：** 3 SP  
**依赖：** P1-01  
**阻塞：** P1-08、P1-11、P1-13  
**Labels：** `phase-1`、`schema`、`yaml`、`validation`

### 用户故事

作为生成文件的消费者，我需要版本化且可严格校验的 YAML 契约，使错误配置在进入 Runtime 或 Dashboard 阶段前被发现。

### 业务价值

- 让生成文件成为稳定 API，而不是只能由当前 CLI 理解的内部文本。
- 支持 CI、未来 Runtime 和第三方工具独立验证文件。
- 防止字段拼写错误、类型漂移和 Renderer 漏字段被静默接受。

### In Scope

- 定义 Metrics、Tracing、Logging 三套 v1 YAML model。
- 提供 JSON Schema 或功能等价的机器可读 Schema。
- 提供严格解析和语义校验 API。
- 固定字段顺序、空集合表达和浮点数序列化规则。

### Out of Scope

- 生成具体 signal 内容。
- 文件系统写入。
- 第三方后端专有配置格式。

### 实现任务

1. 在独立 Generator package 中建立三个强类型 Document model，禁止以通用 map 作为顶层或 signal 定义。
2. 为每个 Document 固定公共 Header：`schema_version`、`document_type`、`source`、`generated_by`。
3. Metrics schema 至少校验 name、type、unit、target、record、value、attributes；Summary 额外校验 quantiles。
4. Tracing schema 至少校验 name、kind、target、lifecycle、parent、attributes、status、events。
5. Logging schema 至少校验 event name、severity、target、trigger、fields、correlation、redaction。
6. 生成或维护机器可读 Schema，例如：
   - `schemas/generator/v1/metrics.schema.json`
   - `schemas/generator/v1/otel.schema.json`
   - `schemas/generator/v1/logging.schema.json`
7. 严格 Decoder 必须拒绝未知字段、重复 YAML key、错误 scalar type、未知 enum 和非有限浮点数。
8. 语义 Validator 检查重复 ID/name、悬空引用、空 required binding、非法 unit、无序/重复 buckets 和 quantiles。
9. 确定序列化规则：使用 typed struct 顺序；定义列表按稳定 ID 排序；禁止 timestamp 和 YAML anchor/alias。
10. 为公共 Header、每种定义和错误路径编写最小有效及无效 fixture。
11. 文档化封闭 Schema 规则：`generator.*/v1` 发布后字段集合固定；新增、删除、改义字段或 enum 均发布新的显式 Schema version。Reader 只接受声明支持的精确版本并拒绝未知字段，不以“可选字段”绕过版本协商。

### 验收标准

#### AC1：有效文档可严格往返

- **Given** 每种输出各一个覆盖全部 v1 字段的有效 fixture
- **When** 严格解析、语义校验、重新渲染并再次解析
- **Then** 两次 model 语义相等且无 Warning

#### AC2：未知字段不被忽略

- **Given** YAML 中包含拼写错误字段 `servce_name`
- **When** 执行严格解析
- **Then** 返回包含文档路径和字段名的 validation error

#### AC3：重复与非法值被拒绝

- **Given** 重复 ID、重复 YAML key、unspecified enum、倒序 bucket 或 `NaN`
- **When** 执行校验
- **Then** 每个 fixture 均失败，错误指出具体实体和规则

#### AC4：输出确定

- **Given** 同一个 Document model
- **When** 渲染 10 次
- **Then** 输出 bytes 完全一致，列表顺序固定且不含环境相关字段

#### AC5：Schema 可被外部工具使用

- **Given** 仓库中的三个机器可读 Schema
- **When** 使用独立 Schema validator 校验有效和无效 fixture
- **Then** 结果与 Go Validator 一致

### 测试要求

- Unit tests：字段级和跨字段语义规则。
- Golden tests：完整 YAML 字节输出。
- Fuzz tests：YAML decoder 不 panic、不无限递归、不接受 duplicate key。
- Contract tests：Go Validator 与机器可读 Schema 对同一 fixture 得到相同 pass/fail 结论。
- Documentation contract test：提取本文所有 `yaml` fenced blocks 并执行语法解析；第 8 节三份输出示例还必须通过各自的 v1 Schema 和语义 Validator。

### 非功能与安全要求

- 单个配置文件最大解析大小设置明确上限，默认建议 10 MiB。
- YAML alias 展开必须受限或禁用，防止资源耗尽。
- Validation error 不回显可能包含秘密的完整字段值。

### Story DoD

- 三个 Schema、Go model、Validator、fixture 和版本演进说明已提交。
- 所有 AC 自动化。
- Schema review 由至少一名 Runtime/Dashboard 潜在消费者完成。

---

## P1-03 实现 Generator Policy 配置与合并规则

**Issue Type：** Story  
**Epic：** P1-CONTRACT  
**优先级：** Highest  
**建议工作量：** 3 SP  
**依赖：** P1-01  
**阻塞：** P1-05  
**Labels：** `phase-1`、`configuration`、`policy`

### 用户故事

作为服务维护者，我需要通过 `si.yaml` 调整生成信号和低风险参数，同时获得严格、可预测的默认值与覆盖顺序。

### 业务价值

- 支持团队在不修改源码和 Generator 代码的情况下采用不同的延迟桶、日志事件和 Span 深度。
- 避免隐式环境变量或宽松 YAML 解析导致不同机器生成不同结果。

### In Scope

- `generation` 配置节点、默认值、严格解析、合并、归一化和校验。
- Metrics、Tracing、Logging 的 Phase 1 可配置项。
- Policy digest 的确定计算，供日志和调试使用，但 digest 不包含秘密。

### Out of Scope

- CLI flags 的注册和文件输出。
- Backend endpoint、token、exporter、sampling pipeline。
- 任意模板语言或用户提供可执行脚本。

### 实现任务

1. 定义强类型 `GenerationConfig` 和不可变 `Policy`，分别表示用户输入和校验后的完整配置。
2. 实现默认值，至少覆盖 signals、output dir、strict、histogram buckets、Summary、internal spans、error events、日志事件和 redaction names。
3. 在现有 `si.yaml` loader 中严格识别 `generation`；未知 generation 字段返回 `GEN_INVALID_CONFIG`。
4. 规定 Merge：CLI 显式 flag 覆盖 YAML；YAML 显式值覆盖默认；未设置 bool 必须使用 pointer/optional 语义，区分 false 与 absent。
5. 归一化 `signals`，只允许 `metrics`、`tracing`、`logging`，去重并按固定顺序保存；空数组表示不生成任何 signal，应作为配置错误而不是成功空操作。
6. 校验 `output_dir` 为相对或显式绝对目录，但不得包含 NUL；实际越界和 symlink 检查由 CLI Writer 执行。
7. 校验 Metrics：
   - namespace 符合命名字符集且长度受限
   - buckets 严格递增、为有限正数、数量在 1 至 50
   - quantiles 在 0 到 1 之间、严格递增、数量在 1 至 10
   - Summary 只有 `enabled=true` 时才读取 quantiles
   - `max_instruments` 与 `max_estimated_series` 为有限正整数且不能高于实现的硬安全上限
8. Tracing 的 semantic convention version 固定为 Generator 内置 `1.37.0`，不暴露用户配置项；若 YAML 出现同名字段，按未知字段拒绝。所有 bool 配置均有明确默认值。
9. 校验 Logging：correlation fields 只能来自 allowlist；redact fields 归一化为小写并去重；禁止配置关闭内置 credential denylist。
10. 使用 canonical JSON 或等价稳定编码计算 Policy digest；不包含 output path，避免相同语义因目录不同而变化。
11. 错误必须包含配置路径，例如 `generation.metrics.histogram_buckets_seconds[2]`，但不输出整个配置内容。
12. 更新配置文档，给出最小配置、完整配置和无效示例。

### 验收标准

#### AC1：零配置获得稳定默认值

- **Given** 一个不含 `generation` 的有效 `si.yaml`
- **When** 构建 Policy
- **Then** 三类 signal 启用，Summary 和 internal call spans 关闭，其余值与文档默认完全一致

#### AC2：覆盖优先级正确

- **Given** 默认值、YAML 和 CLI 对同一字段提供不同值
- **When** 合并 Policy
- **Then** 使用 CLI 值；未在 CLI 设置的字段保留 YAML 值

#### AC3：显式 false 不被默认值覆盖

- **Given** YAML 设置 `emit_completion_events: false`
- **When** 合并默认 Policy
- **Then** 最终值为 false，而不是默认 true

#### AC4：非法范围被拒绝

- **Given** 倒序 bucket、重复 quantile、未知 signal、未知 correlation field 或空 signal 列表
- **When** 加载配置
- **Then** 命令以配置错误失败，并返回准确字段路径

#### AC5：环境不改变 Policy

- **Given** 相同配置及不同环境变量、工作目录和用户名
- **When** 分别构建 Policy
- **Then** Policy 内容和 digest 一致

### 测试要求

- Table-driven tests 覆盖每个字段的 absent、有效、边界和无效值。
- Merge matrix tests 覆盖 defaults/YAML/CLI 三层组合。
- Fuzz tests 覆盖 buckets、quantiles、字段名和 signal list。
- Snapshot test 固定默认 Policy，默认值变化必须经过显式 review。

### 非功能与安全要求

- Config loader 不进行网络请求，不执行 shell，不展开用户模板。
- 不支持在 Generation Policy 中保存后端凭据。
- 错误输出不得包含完整 YAML，防止未来字段中的敏感值泄露。

### Story DoD

- 默认配置、完整示例、校验矩阵和 merge tests 已提交。
- 所有 AC 自动化。
- Product Owner 已确认默认输出行为，SRE 已确认 bucket 和日志默认值。

---

## P1-04 实现统一命名、属性基数与隐私策略

**Issue Type：** Story  
**Epic：** P1-CONTRACT  
**优先级：** Highest  
**建议工作量：** 3 SP  
**依赖：** P1-01  
**阻塞：** P1-05  
**Labels：** `phase-1`、`naming`、`cardinality`、`security`

### 用户故事

作为 SRE 和安全维护者，我需要所有生成信号遵循同一命名、低基数和敏感数据规则，使配置可以安全地进入生产评审。

### 业务价值

- 防止高基数标签造成 Metrics 成本或可用性事故。
- 防止 IR 中的 SQL、URL、Key 等静态值被无意传播到日志和 Trace。
- 为 Dashboard 和查询提供跨服务一致的稳定名称。

### In Scope

- Metric、Span、Log Event 的规范化命名函数。
- 六个统一属性及 signal-specific 属性 allowlist。
- Cardinality class、敏感字段 denylist、URL/route 净化和名称碰撞处理。
- 稳定 Diagnostic。

### Out of Scope

- 对用户运行时数据做内容扫描。
- 企业级自定义数据分类引擎。
- 自动上传或集中管理命名策略。

### 实现任务

1. 建立无状态 `NamingPolicy` 和 `AttributePolicy` 接口，输入 IR 稳定字段，输出规范化值或结构化 Diagnostic。
2. 定义 Metric name 规则：ASCII `snake_case`，以字母开头，只含 `[a-zA-Z0-9_:]` 的允许子集，最大长度明确限制，suffix 表示 unit/type。
3. 定义 Span name 规则：
   - HTTP 使用 `<METHOD> <route-template>`，未知 method 使用 `HTTP <route-template>`
   - gRPC 使用 `<service>/<method>`
   - Cron 使用 `cron <stable-job-name>`
   - Dependency 使用 `<system> <operation>`，不包含原始目标值
4. 定义 Log event name 规则：小写点分段，如 `http.request.completed`、`dependency.operation.failed`。
5. 规范化 service、module、function、operation；保留展示名与机器名的职责边界，不修改 IR 原值。
6. 建立碰撞表：同一 signal 中规范化名称重复时，按 source target ID 排序，并使用 target ID 的 SHA-256 短后缀稳定消歧；输出 `GEN_NAME_COLLISION`。
7. 定义 Metric Attribute allowlist 和 runtime status 有限值；默认只允许 service、operation、status，禁止 raw path、URL、query、SQL、key、payload、error message 进入 Metric。
8. 定义 Trace Attribute allowlist；Endpoint route 可作为静态模板，Dependency URL/host/target/resource/value 全部省略；SQL 只允许 db.system 和 operation。
9. 定义 Logging field allowlist 与不可关闭的 credential denylist；字段名比较忽略大小写及 `-`、`_` 差异。
10. `ValueIsStatic` 仅用于 `Dependency.value`；不得用它推断 target URL/service/resource 的来源。Phase 1 对所有 Dependency target/value/resource 字段均不复制，只绑定安全的 system/operation 或省略，并输出适用的 Diagnostic。
11. 敏感值被移除时只记录字段路径和 target ID，不把被移除值写入 Diagnostic message。
12. 限制每个 Metric 的 attributes 数量和每个 Log event 的 fields 数量；按 buckets、quantiles 和有限属性域计算 instrument/series 上界，超过 Policy 限制返回 `GEN_CARDINALITY_LIMIT_EXCEEDED`，不静默截断。
13. 编写包含 credential、PII、SQL、恶意 URL、超长 Unicode 名称和碰撞名称的安全 fixture。

### 验收标准

#### AC1：统一名称稳定

- **Given** 包含大小写、连字符、空格和 Unicode 的 service/module/operation 名称
- **When** 重复执行规范化
- **Then** 得到合法、相同的机器名，且结果不依赖 locale

#### AC2：碰撞可预测

- **Given** 两个不同 target 规范化为同一名称
- **When** 生成 signal 名称
- **Then** 两者使用由各自稳定 ID 派生的确定后缀，并产生一个可定位的 `GEN_NAME_COLLISION`

#### AC3：高基数 Metrics 属性被阻止

- **Given** IR 包含动态 URL、SQL、Redis Key 和 Kafka payload 测试值
- **When** 生成 Metric attributes
- **Then** 输出中不存在这些值，并为每个被请求但禁止的绑定产生不泄漏原值的 Diagnostic

#### AC4：Dependency Target 不进入 Trace

- **Given** 静态 URL `https://user:pass@example.com/orders?id=42#detail`
- **When** 创建 HTTP Client Trace attributes
- **Then** 完整 URL、scheme、host、userinfo、query 和 fragment 均不进入 Plan，只保留受控 HTTP operation

#### AC5：Credential denylist 不可关闭

- **Given** 用户配置尝试从 redaction list 中移除 `authorization` 或 `password`
- **When** 构建 Policy
- **Then** 配置失败并指出内置 denylist 不可放宽

#### AC6：strict 模式提升 Warning

- **Given** 发生名称碰撞或属性被阻止
- **When** 使用 strict Policy 规划
- **Then** 规划失败且不产生可提交文件

#### AC7：Series Budget 可执行

- **Given** 一个按配置 buckets 和 status 域计算后超过 `max_estimated_series` 的 IR
- **When** 规划 Metrics
- **Then** 返回 `GEN_CARDINALITY_LIMIT_EXCEEDED`，报告估算值、上限和 signal，不返回被静默截断的 Plan

### 测试要求

- Table-driven naming tests 覆盖所有 Endpoint/Dependency kind。
- Property/fuzz tests 验证输出始终满足字符集和长度限制。
- Security regression fixture 明确断言生成 bytes 和 Diagnostic 中均不存在 canary secrets。
- Collision determinism test 改变输入顺序后仍得到相同名称映射。

### 非功能与安全要求

- 名称和属性处理总复杂度不高于 $O(N \log N)$。
- Hash 只用于稳定消歧，不用于保护秘密；任何秘密必须直接丢弃。
- 不读取进程环境、Git 配置或网络来补全名称。

### Story DoD

- 命名规范、allowlist、denylist 和碰撞策略有公开文档。
- 安全 fixture 经 Security Reviewer 检查。
- 所有 AC 自动化并通过。

---

## P1-05 建立确定性 Planner 与 IR 校验

**Issue Type：** Story  
**Epic：** P1-CONTRACT  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P1-01、P1-03、P1-04  
**阻塞：** P1-06、P1-07、P1-09、P1-10、P1-12  
**Labels：** `phase-1`、`planner`、`validation`、`determinism`

### 用户故事

作为 Signal Generator 开发者，我需要一个统一 Planner 校验输入 IR、应用 Policy 并协调各信号规划器，使相同输入总能得到相同且引用完整的 Generation Plan。

### 业务价值

- 将结构校验、错误聚合、排序和 strict 行为集中管理，避免三个 Generator 各自实现不同规则。
- 让错误在渲染或文件写入之前暴露，避免产出部分有效配置。
- 提供清晰扩展点，未来新增 Dashboard 或后端 Renderer 时不需要读取 AST。

### 背景与问题

Phase 0 IR 可能包含 non-fatal Analyzer Diagnostic、未知动态值或因旧版本产生的不完整引用。Planner 必须区分“结构不可用”和“信息不完整但可安全降级”，并保证所有降级都有 Diagnostic。

### In Scope

- Planner 公共接口和各 signal 子规划器接口。
- IR compatibility、结构、引用和能力校验。
- Diagnostic 聚合、strict 提升、稳定 ID、排序和取消处理。
- 空 signal、部分实体和 non-fatal source Diagnostic 的明确行为。

### Out of Scope

- 具体 Metric、Span、Log 映射。
- YAML 渲染和文件系统写入。
- 修复或补猜 Analyzer 未能证明的代码事实。

### 输入与输出

建议接口语义：

```go
type Planner interface {
   Plan(ctx context.Context, document *observabilityv1.ObservabilityDocument, policy Policy) (*observabilityv1.GenerationPlan, Report, error)
}
```

- `GenerationPlan` 只在没有 fatal error 时返回。
- `Report` 包含 source 和 generator diagnostics 的分组计数，不保存敏感值。
- `error` 表示调用无法得到可提交 Plan；Warning 通过 Report 表达。

### 实现任务

1. 创建只依赖 `ir/v1` 和 Generator Policy 的 Planner package；增加 import boundary test。
2. 在规划前检查 `context.Context`，长列表处理期间定期检查取消，不在取消后继续生成。
3. 校验输入非 nil、Service 存在、service name 非空、IR schema 在支持范围内。
4. 一次遍历建立 Function、Endpoint、Dependency、CallEdge ID index，检查空 ID 和重复 ID。
5. 校验所有引用：
   - Endpoint/Dependency 的 `function_id` 存在
   - Function 中 endpoint/dependency/caller/callee IDs 存在
   - CallEdge caller/callee 与 resolution 一致
   - 每个 Plan target 的 kind 与 Endpoint、Function、Dependency、CallEdge ID index 类型一致
6. 结构性错误使用 `GEN_INVALID_IR` 或 `GEN_DANGLING_REFERENCE` 并立即阻止 Plan；不得 panic。
7. 对可降级信息建立能力判断，例如动态 target、未知 operation、缺失 source location；保留实体并产生 Warning，或在不安全时跳过该 signal item。
8. 定义稳定 Plan ID 规则：由 signal、target ID 和 purpose 构成；名称变化不得改变 Plan ID。
9. 注册 Metrics、Tracing、Logging 子规划器；只调用 Policy 启用的 signal，并保证调用顺序不影响最终排序。
10. 子规划器返回 items 和 diagnostics，不直接修改共享 Plan；主 Planner 在单一位置合并。
11. 合并前检查跨子规划器 ID 重复、target type 不一致和不受支持的 binding source。
12. 对 Plan 的 metrics、spans、logs、attributes、events 和 diagnostics 使用稳定比较器排序。
13. strict 模式下，将任何 Generator Warning 提升为失败；Phase 0 source Warning 仍保留原级别，除非它导致目标无法安全规划。
14. 不修改输入 `ObservabilityDocument`；如需排序必须复制 slice。
15. 不读取源码文件、工作目录、环境变量、Git 信息或网络。
16. 记录规划摘要：输入实体数、各 signal item 数、跳过数、各级 Diagnostic 数。

### 验收标准

#### AC1：有效 IR 产生完整 Plan

- **Given** 一个包含 Service、HTTP Endpoint、SQL Dependency 和有效引用的 v1 IR
- **When** 使用默认 Policy 规划
- **Then** 返回非 nil Plan、无 fatal error，三个 signal 子规划器均收到对应实体

#### AC2：悬空引用阻止规划

- **Given** Dependency 引用不存在的 Function ID
- **When** 调用 Planner
- **Then** 返回 `GEN_DANGLING_REFERENCE`、实体 ID 和字段名，不返回部分 Plan

#### AC3：动态信息安全降级

- **Given** HTTP Client 的 target URL 为动态未知值但实体引用完整
- **When** 在非 strict 模式规划
- **Then** 保留通用 client signal 定义，省略目标属性，返回 `GEN_INCOMPLETE_TARGET`

#### AC4：strict 模式不提交 Warning Plan

- **Given** 与 AC3 相同输入
- **When** 在 strict 模式规划
- **Then** 返回失败且不返回可提交 Plan

#### AC5：输入顺序不影响结果

- **Given** 两份语义相同但 Function、Endpoint、Dependency 和 CallEdge 顺序不同的 IR
- **When** 使用相同 Policy 规划并确定性序列化
- **Then** Plan bytes 和 Report 计数完全一致

#### AC6：取消及时生效

- **Given** 一个已取消的 Context 和大型 IR fixture
- **When** 调用 Planner
- **Then** 返回 `context.Canceled`，不调用 Renderer，不产生 Plan

#### AC7：输入不被修改

- **Given** 一份未排序 IR 及其确定性序列化快照
- **When** 调用 Planner
- **Then** 调用前后的输入快照完全相同

### 测试要求

- Unit tests：nil、空 Service、未知 schema、重复 ID、每类悬空引用、动态值、strict、取消。
- Permutation/property tests：随机改变 slice 顺序，Plan 保持一致。
- Race tests：并行对同一只读 IR 规划，不出现 data race。
- Import boundary test：Planner package 的依赖图中不出现 Analyzer/AST 包。
- Benchmark：分别记录 100、1,000、10,000 个实体的规划时间和分配量。

### 非功能要求

- 建立索引和引用校验的平均复杂度为 $O(N)$，最终排序不高于 $O(N \log N)$。
- 不使用包级可变 registry；扩展点通过构造函数注入。
- Error wrapping 必须保留阶段、signal 和 target ID。

### Story DoD

- Planner 接口、验证器、Report、Diagnostic 映射和 tests 已完成。
- 所有 AC 自动化。
- Benchmark 已记录但不以未经校准的绝对阈值阻止首次合并。

---

## P1-06 生成 Endpoint Metrics Plan

**Issue Type：** Story  
**Epic：** P1-METRICS  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P1-05  
**阻塞：** P1-08  
**Labels：** `phase-1`、`metrics`、`endpoint`

### 用户故事

作为服务维护者，我需要为 HTTP、gRPC 和 Cron 入口自动获得请求/执行数量、耗时和运行中数量定义，使每个入口具备一致的基本健康信号。

### 业务价值

- 自动覆盖 RED 方法中的 Rate、Errors、Duration，并为并发压力提供 Gauge。
- 为 Phase 2 Dashboard 提供稳定的 Metric ID、名称、单位和属性。

### In Scope

- HTTP Handler、gRPC Handler、Cron Job 的 Counter、Histogram、Gauge Plan。
- 启用 Summary 时为耗时创建 Summary Plan。
- Record trigger、runtime value、status 和统一属性 Binding。

### Out of Scope

- Dependency 指标。
- 运行时 Metric SDK 调用。
- 从原始 URL 或请求中提取标签。

### 默认指标映射

| Endpoint | Counter | Histogram | Gauge | 可选 Summary |
| --- | --- | --- | --- | --- |
| HTTP | `http_requests_total` | `http_request_duration_seconds` | `http_requests_in_flight` | `http_request_duration_seconds_summary` |
| gRPC | `grpc_requests_total` | `grpc_request_duration_seconds` | `grpc_requests_in_flight` | `grpc_request_duration_seconds_summary` |
| Cron | `cron_runs_total` | `cron_run_duration_seconds` | `cron_jobs_in_flight` | `cron_run_duration_seconds_summary` |

实际名称使用 P1-04 的 service/module/function/operation 前缀规则，表中为 purpose suffix。

### 实现任务

1. 为三种 EndpointKind 建立显式 mapping table；未知 kind 返回 `GEN_UNSUPPORTED_ENTITY`，禁止 default 猜测。
2. 为每个 Endpoint 生成完成时递增的 Counter，value 为常量 `1`，并绑定有限 `runtime.operation.status`。
3. 为每个 Endpoint 生成 Histogram，value 为 `runtime.operation.duration_seconds`，unit 为 `s`，使用 Policy buckets。
4. Policy 启用 in-flight 时生成 Gauge，trigger 为 lifecycle state change，value 为 `runtime.operation.in_flight`；值不得由 Generator 静态计算。
5. Policy 启用 Summary 时，使用相同 duration source 和已校验 quantiles 生成独立 Summary。
6. 每个 Plan 通过 `target_id` 引用 Endpoint，并包含所属 Function 的稳定引用。
7. Metrics 默认 Attribute 只绑定 service、operation、status；module/function 用于稳定 instrument name 和 target metadata，version 不进入默认 Metrics。静态值在 Plan 中固定，runtime 值使用强类型 Binding。
8. HTTP operation 使用静态 method 和 route template；未知 method/path 使用受控 fallback 并产生 `GEN_INCOMPLETE_TARGET`。
9. gRPC operation 使用 service/method；缺失任一字段时使用 Function identity 作为 fallback，并产生 Diagnostic。
10. Cron operation 使用稳定 job name；schedule 仅作为受控静态元数据，不进入默认 Metric attributes。
11. 生成 description，说明对象、unit 和记录时机；description 不包含绝对路径或源代码片段。
12. 所有 Metric ID、name 和 attributes 在返回前使用统一策略校验和排序。
13. 按 status 域、Histogram buckets 和 Summary quantiles 计算 instrument/series 上界；超过任一 Policy 限制时整体失败，不返回部分 Endpoint Metrics。

### 验收标准

#### AC1：HTTP 默认生成三类指标

- **Given** 一个 method 为 POST、route 为 `/orders/{id}` 的 HTTP Endpoint
- **When** 使用默认 Policy 规划 Metrics
- **Then** 生成一个 Counter、一个 seconds Histogram、一个 in-flight Gauge，均引用该 Endpoint

#### AC2：gRPC 属性使用静态服务和方法

- **Given** `OrderService/CreateOrder` gRPC Endpoint
- **When** 规划 Metrics
- **Then** operation 由静态 service/method 构成，不包含请求内容，status 绑定到有限 runtime enum

#### AC3：Cron 不产生高基数标签

- **Given** 一个含 schedule 的 Cron Endpoint
- **When** 规划 Metrics
- **Then** 生成 run count、duration、in-flight，Metric attributes 不包含执行时间、参数或动态 job payload

#### AC4：Summary 默认关闭且可显式开启

- **Given** 同一个 Endpoint
- **When** 分别使用默认 Policy 和 `summaries.enabled=true`
- **Then** 默认无 Summary；启用后增加且仅增加一个使用配置 quantiles 的 duration Summary

#### AC5：未知路由安全降级

- **Given** HTTP Endpoint 缺少 method 或 route
- **When** 在非 strict 模式规划
- **Then** 生成不含 raw path 的通用指标并返回 `GEN_INCOMPLETE_TARGET`

#### AC6：输入顺序不影响指标

- **Given** 相同 Endpoint 集合的不同顺序
- **When** 规划并序列化 Metric Plans
- **Then** ID、name、description、attributes 和顺序完全一致

### 测试要求

- Table-driven tests：HTTP、gRPC、Cron、未知 kind、字段缺失。
- Policy matrix：Gauge on/off、Summary on/off、自定义 buckets/quantiles。
- Cardinality assertions：禁止 raw URL/query、source location 和 schedule execution values。
- Unit assertions：Counter 为 `{request}` 或 `{operation}` 等明确单位，duration 为 `s`，Gauge 为 `{operation}`。
- Golden Plan test：固定一个 composite Endpoint fixture。

### 非功能要求

- 每个 Endpoint 生成的默认 Metric 数量有固定上限，禁止随 CallEdge 数量隐式增长。
- Planner 无共享状态，可并行处理不同 Document。

### Story DoD

- 三种 Endpoint mapping、所有 metric type 路径和 tests 完成。
- SRE review 确认名称、单位、bucket 和 attributes。
- 所有 AC 自动化。

---

## P1-07 生成 Dependency Metrics Plan 和全部指标类型

**Issue Type：** Story  
**Epic：** P1-METRICS  
**优先级：** High  
**建议工作量：** 5 SP  
**依赖：** P1-05  
**阻塞：** P1-08  
**Labels：** `phase-1`、`metrics`、`dependency`、`cardinality`

### 用户故事

作为服务维护者，我需要为 Kafka、SQL、Redis、HTTP Client 和 RPC Client 自动生成一致的依赖操作指标，使外部系统的延迟和失败可以被独立观察。

### 业务价值

- 自动覆盖依赖调用的 Rate、Errors 和 Duration。
- 通过统一 operation 和 system 分类支持未来依赖 Dashboard，而不暴露目标资源细节。

### In Scope

- 六种 Phase 0 DependencyKind 的 Counter、Histogram 和可选 Gauge/Summary。
- 静态/动态 operation 与 target 的安全处理。
- 同一 Function 多调用点、重复 operation 和未知 target 的确定行为。

### Out of Scope

- 连接池大小、队列深度等 Phase 0 IR 无法证明的业务 Gauge。
- SQL statement、Redis key、message body 作为指标标签。
- 聚合不同服务的指标或下发 Prometheus recording rules。

### 默认依赖映射

| DependencyKind | `system` | operation 来源 | 默认指标 purpose |
| --- | --- | --- | --- |
| `KAFKA_PRODUCER` | `kafka` | producer API operation | operations、duration |
| `KAFKA_CONSUMER` | `kafka` | consumer API operation | operations、duration |
| `SQL` | `sql` 或可证明的 db system | query/exec/prepare/begin 等 | operations、duration |
| `REDIS` | `redis` | get/set/del 等 | operations、duration |
| `HTTP_CLIENT` | `http` | GET/POST/Do 等受控 operation | operations、duration |
| `RPC_CLIENT` | `rpc` | 静态 service/method 或 method | operations、duration |

### 实现任务

1. 建立 exhaustive DependencyKind mapping，新增 enum 未处理时测试必须失败。
2. 每个 Dependency 默认生成操作 Counter 和 duration Histogram；Counter 使用有限 status 属性表达成功/失败。
3. `include_in_flight_gauges=true` 时生成依赖操作 in-flight Gauge；明确它表示调用中操作数，不表示连接池或队列深度。
4. Summary 启用时生成 duration Summary，使用与 Endpoint 相同的 quantile Policy。
5. 归一化 operation；缺失 operation 时使用受控 `unknown` 并产生 `GEN_INCOMPLETE_TARGET`。
6. 只使用 DependencyKind 推导 system；不得仅根据方法名猜测第三方系统。
7. Kafka topic、consumer group 和 destination 在 Phase 1 不进入任何 Metric metadata 或 attributes，不以 `ValueIsStatic` 放宽该规则。
8. SQL 仅生成 system 和 operation，不复制 SQL text、table、参数或 source value。
9. Redis 不复制 key、value 或动态 resource。
10. HTTP Client 不复制完整 URL；默认 Metric 仅保留受控 method/operation，不包含 host、path、query。
11. RPC Client 不复制 target service 或 address；Metric name/attributes 仅使用 kind、受控 operation 和公共低基数词汇。
12. 同一 Function 中不同 source location 的 Dependency 保持不同 Plan ID；相同规范化 name 由 P1-04 稳定消歧。
13. 为动态 target 保留通用 Dependency metrics，并产生不含原始值的 `GEN_INCOMPLETE_TARGET`。
14. 限制每个 Dependency 生成数量；Policy 关闭 Gauge/Summary 后不得留下空定义。
15. 验证四种 Metric type 在 Endpoint + Dependency 组合 Plan 中均可表达和序列化。
16. 与 Endpoint Metrics 合并后统一计算 `max_instruments` 和 `max_estimated_series`；超过上限返回实际估算和配置上限。

### 验收标准

#### AC1：每种 Dependency 生成基础指标

- **Given** 一个包含六种 DependencyKind 的 composite IR
- **When** 使用默认 Policy 规划 Metrics
- **Then** 每个 Dependency 各有 operations Counter 和 seconds Histogram，system/operation 映射正确

#### AC2：敏感资源不进入指标

- **Given** SQL text、Redis key、带凭据 URL 和 Kafka payload canary
- **When** 规划 Metrics
- **Then** Plan name、description、attributes 和 diagnostics 均不包含 canary 值

#### AC3：动态 target 不丢失基础可观测性

- **Given** `ValueIsStatic=false` 的 HTTP Client 和 Kafka Dependency
- **When** 在非 strict 模式规划
- **Then** 仍生成 system/operation 基础指标，省略 target 值，并产生 `GEN_INCOMPLETE_TARGET`

#### AC4：多调用点保持独立来源

- **Given** 同一 Function 在两个位置执行相同 Redis operation
- **When** 规划 Metrics
- **Then** 两组 Plan ID 可追溯到不同 Dependency ID，名称冲突按统一策略处理且结果确定

#### AC5：Gauge 语义不越界

- **Given** 任意 SQL 或 Kafka Dependency
- **When** 生成 in-flight Gauge
- **Then** value source 仅为 `runtime.operation.in_flight`，不宣称连接池、队列长度或 consumer lag

#### AC6：所有指标类型可完整校验

- **Given** 开启 Gauge 和 Summary 的 composite fixture
- **When** 生成并调用 Plan Validator
- **Then** Counter、Histogram、Gauge、Summary 均通过，unit、trigger、value source 和 policy 参数完整

### 测试要求

- 每个 DependencyKind 的正常、动态值、缺失 operation 和敏感值测试。
- Negative non-match fixture：未知 dependency enum 不得被猜测为已支持类型。
- Canary secret tests 同时扫描 Plan、Diagnostic 和序列化 bytes。
- Permutation tests 和同名多调用点 tests。
- Golden Plan 覆盖全部 Endpoint 与 Dependency metrics。

### 非功能与安全要求

- 每个 Dependency 默认最多生成三个 Metric；启用 Summary 后最多四个。
- 任何 target value 进入 attribute 前必须通过统一 policy，禁止 Renderer 绕过。

### Story DoD

- 六类依赖、四种指标类型、动态降级和安全 tests 完成。
- 所有 AC 自动化。
- Metrics Plan 可由 P1-02 Validator 完整校验。

---

## P1-08 渲染并校验 `metrics.yaml`

**Issue Type：** Story  
**Epic：** P1-METRICS  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P1-02、P1-06、P1-07  
**阻塞：** P1-14  
**Labels：** `phase-1`、`metrics`、`renderer`、`yaml`

### 用户故事

作为 Runtime 或配置评审者，我需要一个稳定、可读且符合 Schema 的 `metrics.yaml`，使指标计划能够被机器消费并纳入版本控制。

### 业务价值

- 将语言无关 Plan 转换为公开文件契约。
- 通过渲染后再次校验，防止 Renderer bug 产生无效配置。

### In Scope

- Metrics Plan 到 typed YAML model 的无状态转换。
- Canonical YAML 渲染、Schema/语义复验和 Golden tests。
- 空 Metrics、四种 type、attributes 和 policy 参数的明确表达。

### Out of Scope

- 文件写入、目录创建、覆盖策略。
- Prometheus scrape/exporter 配置。
- Runtime SDK 代码生成。

### 实现任务

1. 提供 `RenderMetrics(plan, policy) ([]byte, error)` 或等价接口；输入 nil、schema 不兼容或含非 Metrics target 时返回上下文错误。
2. 只从已验证 MetricPlan 复制字段，不重新推导名称、属性或安全策略。
3. 构造公共 Header，`document_type` 固定为 `instrumentation.metrics`，记录 IR 和 Generator schema version。
4. 使用 typed YAML structs 保持字段顺序；definitions 按 Plan ID 排序，attributes 按 key 排序。
5. 明确输出 Counter、Histogram、Gauge、Summary 的 type、unit、description、target、record trigger、value binding。
6. Histogram 输出 canonical buckets；Summary 输出 canonical quantiles；浮点数采用稳定、可往返格式。
7. required runtime binding 必须显式包含 fallback/error behavior；不得把占位字符串伪装为静态值。
8. 不输出空可选字段、YAML anchor、alias、timestamp、注释中的机器信息或绝对路径。
9. 渲染到内存后，立即使用 P1-02 strict decoder 和 semantic validator 重新解析。
10. Renderer/Validator 失败返回 `GEN_RENDER_ERROR` 上下文，不返回部分 bytes。
11. 为四种 type、无 Metrics、动态 fallback、名称碰撞后的 Plan 建立 Golden YAML。
12. 记录文件契约示例及字段到 Runtime binding source 的说明。

### 验收标准

#### AC1：完整 Metrics 文件有效

- **Given** 一个包含四种 Metric type 的有效 Plan
- **When** 渲染 `metrics.yaml`
- **Then** 输出可由严格 Decoder 和机器可读 Schema 验证，所有 Plan ID 各出现一次

#### AC2：渲染字节稳定

- **Given** 同一 Plan 和 Policy
- **When** 在不同工作目录连续渲染 10 次
- **Then** SHA-256 和 bytes 完全一致

#### AC3：Renderer 不补业务逻辑

- **Given** Plan 中没有 Summary
- **When** Policy 中曾启用 Summary 但 Plan 未包含该项
- **Then** Renderer 不自行创建 Summary，而是按 Plan 渲染并由一致性校验报告问题

#### AC4：非法 Plan 无部分输出

- **Given** Metric 缺少 value binding 或使用未知 type
- **When** 调用 Renderer
- **Then** 返回 `GEN_RENDER_ERROR`，结果 bytes 为 nil/empty

#### AC5：输出不泄漏环境信息

- **Given** IR source root、用户名和环境变量包含 canary 值
- **When** 渲染 Metrics
- **Then** 输出 bytes 不包含任何 canary

### 测试要求

- Unit tests：nil/invalid Plan、每种 type、empty optional fields。
- Golden tests：完整文件字节比较。
- Contract tests：渲染结果同时通过 Go Validator 和外部 Schema validator。
- Cross-platform test：换行固定为 LF，路径字段如存在统一使用 `/`。
- Benchmark：预构造 1,000 个 MetricPlan 的纯渲染时间和 allocations。

### 非功能要求

- Renderer 为纯函数语义，不访问文件系统、网络、环境变量或当前时间。
- 对 $N$ 个 Metric 的渲染复杂度不高于 $O(N \log N)$。

### Story DoD

- Renderer、契约示例、Golden tests 和 benchmark 已提交。
- 所有 AC 自动化。
- `metrics.yaml` 可供 Runtime 消费者独立解析。

---

## P1-09 生成 Endpoint Root Span Plan

**Issue Type：** Story  
**Epic：** P1-TRACING  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P1-05  
**阻塞：** P1-10、P1-11  
**Labels：** `phase-1`、`tracing`、`opentelemetry`、`endpoint`

### 用户故事

作为服务维护者，我需要为 HTTP、gRPC 和 Cron 入口自动获得规范的 Root Span 定义，使一次请求或任务执行具备稳定的 Trace 起点。

### 业务价值

- 自动建立 Trace 根边界和上下文传播策略。
- 统一 Span name、kind 和语义属性，便于跨服务查询。

### In Scope

- HTTP/gRPC/Cron Endpoint Root Span。
- Start/end trigger、remote context extraction 或 new root 策略。
- 静态安全 attributes 和 runtime status binding。

### Out of Scope

- Dependency/Internal Child Span。
- SDK Propagator 实现和采样策略。
- Kafka message handler root 推断。

### 默认 Root Span 映射

| Endpoint | Span Kind | Name | Parent Strategy |
| --- | --- | --- | --- |
| HTTP | `SERVER` | `<METHOD> <route-template>` | 从受支持 HTTP carrier 提取，缺失时创建 root |
| gRPC | `SERVER` | `<service>/<method>` | 从 gRPC metadata 提取，缺失时创建 root |
| Cron | `INTERNAL` | `cron <stable-job-name>` | 始终创建新 root，除非 Runtime 显式提供 context |

### 实现任务

1. 对三种 EndpointKind 建立 exhaustive Root Span mapping。
2. Span ID 由 target Endpoint ID 和 `root` purpose 稳定派生；Span name 使用 P1-04 规则。
3. HTTP Span lifecycle 绑定 handler entry/exit；parent strategy 为 `extract_or_root`，carrier 类型为 HTTP headers 的抽象标识。
4. gRPC Span lifecycle 绑定 server method entry/exit；parent strategy 为 `extract_or_root`，carrier 为 gRPC metadata 抽象标识。
5. Cron lifecycle 绑定 job callback entry/exit；默认 `new_root`，不得假设 HTTP/RPC carrier。
6. 使用 Phase 1 固定的 OpenTelemetry Semantic Conventions `1.37.0` 映射属性；不得写 `latest` 或版本范围。
7. HTTP 允许属性：受控 method、route template、server/system 常量；禁止 query、headers、body 和 raw URL。
8. gRPC 允许属性：`rpc.system=grpc`、service、method；禁止 request/response payload。
9. Cron 允许属性：稳定 job name 和静态 schedule；禁止运行参数。
10. service/module/function/operation/version 逻辑词汇映射为标准 resource/code/operation attributes，避免同时输出重复 alias。
11. 为 end trigger 绑定 `runtime.operation.status` 和 duration；具体 error events 由 P1-10 添加。
12. Endpoint 字段缺失时使用稳定 Function identity fallback 并产生 `GEN_INCOMPLETE_TARGET`；strict 模式失败。
13. 不为普通 Function 或 Dependency 创建 root，避免重复 Trace 根。

### 验收标准

#### AC1：HTTP Root Span 正确

- **Given** POST `/orders/{id}` Endpoint
- **When** 规划 Tracing
- **Then** 生成一个 `SERVER` Span，name 为受控 method + route template，parent 为 `extract_or_root`

#### AC2：gRPC Root Span 正确

- **Given** `OrderService/CreateOrder` Endpoint
- **When** 规划 Tracing
- **Then** 生成一个 `SERVER` Span，包含 rpc system/service/method，不包含 payload

#### AC3：Cron 使用新 Root

- **Given** Cron Endpoint
- **When** 规划 Tracing
- **Then** 生成一个 `INTERNAL` Span，默认 parent strategy 为 `new_root`，包含稳定 job identity

#### AC4：不猜测 Kafka Handler

- **Given** 只有 `KAFKA_CONSUMER` Dependency 而没有对应 Endpoint
- **When** 规划 Root Spans
- **Then** 不创建 Kafka handler root；该实体留给 Dependency Span 规则处理

#### AC5：敏感请求信息不进入 Plan

- **Given** IR 中 target URL 含 userinfo/query canary
- **When** 规划 HTTP Root Span
- **Then** Span attributes、name 和 diagnostics 不包含 canary

#### AC6：缺失 route 可诊断降级

- **Given** HTTP Endpoint route 为空
- **When** 非 strict 规划
- **Then** 使用稳定 fallback name，产生 `GEN_INCOMPLETE_TARGET`，不使用 source path 作为 name

### 测试要求

- HTTP、gRPC、Cron 正向和字段缺失 tests。
- Parent strategy tests。
- Semantic attribute allowlist tests。
- Negative Kafka root test。
- Golden Root Span Plan 和 input permutation test。

### 非功能与安全要求

- 不读取实际 carrier 或运行时请求；Plan 只描述 binding。
- 每个 Endpoint 最多生成一个 Root Span Plan。

### Story DoD

- 三类 Root Span、context strategy、attributes 和 tests 完成。
- OTel 语义约定版本已固定并文档化。
- 所有 AC 自动化。

---

## P1-10 生成 Child Span、Attributes、Status 和 Events

**Issue Type：** Story  
**Epic：** P1-TRACING  
**优先级：** High  
**建议工作量：** 5 SP  
**依赖：** P1-09  
**阻塞：** P1-11  
**Labels：** `phase-1`、`tracing`、`dependencies`、`errors`

### 用户故事

作为服务维护者，我需要外部依赖和可选内部调用自动生成 Child Span、状态和错误事件，使 Trace 能解释请求时间消耗在哪里以及为何失败。

### 业务价值

- 将 SQL、Redis、Kafka、HTTP/RPC Client 延迟定位到具体调用类型。
- 建立一致错误状态和事件模型，避免每个依赖使用不同字段。

### In Scope

- 六种 DependencyKind 的 Child Span mapping。
- Policy 控制的 resolved internal call Span。
- Endpoint Root Span 的 status、exception、timeout 和 cancellation events。
- 当前上下文 parent、静态安全 attributes、status mapping、exception/timeout/cancel events。

### Out of Scope

- 采集完整 error message、stacktrace、SQL statement 或 payload。
- 推断跨 goroutine 的运行时 Context 传播。
- 未解析动态调用的虚假 Child Span。

### 默认 Child Span 映射

| DependencyKind | Span Kind | Name 基础 | 允许的专用属性 |
| --- | --- | --- | --- |
| `KAFKA_PRODUCER` | `PRODUCER` | `kafka <operation>` | 仅 messaging system；不输出 destination/topic/group |
| `KAFKA_CONSUMER` | `CONSUMER` | `kafka <operation>` | messaging system；明确标识为调用点而非 handler root |
| `SQL` | `CLIENT` | `db <operation>` | db system、operation |
| `REDIS` | `CLIENT` | `redis <operation>` | db system、operation |
| `HTTP_CLIENT` | `CLIENT` | `HTTP <method>` | method；不输出 server address |
| `RPC_CLIENT` | `CLIENT` | `rpc <operation>` | rpc system/operation；不输出 target service/address |
| Resolved Internal Call | `INTERNAL` | `<qualified-function>` | code namespace/function；默认关闭 |

### 实现任务

1. 建立 exhaustive DependencyKind 到 Span Kind、name 和 attribute provider 的映射。
2. 每个 Dependency Span 使用 `parent=current_context`，不把静态 Call Graph 当成唯一运行时 parent。
3. lifecycle 绑定 Dependency 调用前后，duration 和 result 由 runtime binding 提供。
4. Kafka Consumer Span 明确标记 `scope=client_call` 或等价枚举，不宣称消息 handler root。
5. SQL 只使用 system/operation；永不复制 statement、参数、table 猜测或 `Dependency.Value`。
6. Redis 只使用 system/operation；永不复制 key/value。
7. HTTP Client 只保留受控 method/operation；server address、URL、path 和 query 一律省略。
8. RPC Client 只使用受控 operation；target service/address 一律省略，信息不足时产生 `GEN_INCOMPLETE_TARGET`。
9. Kafka destination、topic、consumer group 和 payload 一律省略；`ValueIsStatic` 不得用于放宽该规则。
10. 当 `include_internal_calls=true` 时，只为 `RESOLVED` CallEdge 生成引用该 CallEdge ID 的 `INTERNAL` Span；忽略 unresolved edge 并产生能力 Diagnostic。
11. 处理递归和互递归 CallEdge：按 edge ID 生成有限定义，不递归展开调用图。
12. 将 runtime status 映射为 OTel status：
   - `ok` -> `UNSET`
   - `error`、`timeout` -> `ERROR`
   - `cancelled` -> `ERROR`，并记录受控 cancellation event/attribute
   - `unknown` -> `UNSET`
13. 对 P1-09 生成的每个 Endpoint Root Span 应用相同 status mapping；`record_exception_events=true` 时为 Root 和 Dependency Span 的 error 生成 `exception` event，默认只绑定安全的 error type。
14. error message 和 stacktrace 默认不绑定；为 timeout/cancelled 创建受控 event 或 attributes，不把原始错误字符串作为 event name。
15. 事件、attributes 和 status rules 按 key/ID 稳定排序，重复 key 在 Plan validation 阶段失败。
16. Span name 和 attributes 全部通过 P1-04 policy；Renderer 不再做敏感值补救。

### 验收标准

#### AC1：六类依赖 Span Kind 正确

- **Given** 包含六种 DependencyKind 的 composite IR
- **When** 规划 Tracing
- **Then** Producer/Consumer/Client Kind 与映射表一致，每个 Span 引用正确 Dependency ID

#### AC2：当前 Context 作为 Parent

- **Given** 一个由多个 Root Endpoint 可达的共享 SQL Function
- **When** 规划 SQL Span
- **Then** Parent strategy 为 `current_context`，不固定绑定某一个静态 Root Span ID

#### AC3：Internal Span 默认关闭

- **Given** resolved CallEdges
- **When** 使用默认 Policy 与 `include_internal_calls=true` 分别规划
- **Then** 默认不生成 Internal Span；开启后每个符合条件 edge 生成有限且确定的定义

#### AC4：递归调用不无限展开

- **Given** 自递归和互递归 CallEdges
- **When** 开启 Internal Span 规划
- **Then** 每个静态 edge 最多生成一个 Plan，规划正常结束

#### AC5：错误事件不泄漏消息

- **Given** Endpoint Root 和 Dependency Span 的 runtime error message 中可能包含 secret canary
- **When** 使用默认 Policy 生成 exception event
- **Then** 两类 Span 都仅包含 error type/status binding，不包含 message 或 stacktrace binding

#### AC6：动态目标安全降级

- **Given** 动态 HTTP URL、RPC target 和 Kafka topic
- **When** 非 strict 规划
- **Then** 创建通用 Span，完整省略 URL/target/topic 属性，并产生不泄漏值的 Diagnostic

#### AC7：Status mapping 完整

- **Given** `ok`、`error`、`timeout`、`cancelled`、`unknown` 五种 runtime status
- **When** 校验生成的 status rule
- **Then** 每个输入都有唯一、文档化的输出，不存在 default fallthrough

#### AC8：Root Span 状态和事件完整

- **Given** HTTP、gRPC、Cron Root Span 各自发生 error、timeout 和 cancelled
- **When** 规划 Tracing
- **Then** 每个 Root Span 都有完整 status mapping 和受控事件，且 event target 仍引用原 Endpoint Span

### 测试要求

- 每个 DependencyKind 的 kind/name/attribute tests。
- Shared Function parent strategy test。
- Internal Call on/off、unresolved、recursive tests。
- Status table tests，以及 Endpoint Root/Dependency exception、timeout、cancelled event tests。
- Canary URL/SQL/key/topic/error message security tests。
- Golden composite Span Plan。

### 非功能与安全要求

- 对 CallEdge 只做单次遍历和排序，不执行路径枚举，避免指数复杂度。
- 默认每个 Dependency 最多一个 Span；每个 error policy 的 event 数量有固定上限。

### Story DoD

- Dependency/Internal Spans、status、events 和 security tests 完成。
- 所有 AC 自动化。
- Trace Plan 可由 P1-01 Validator 完整校验。

---

## P1-11 渲染并校验 `otel.yaml`

**Issue Type：** Story  
**Epic：** P1-TRACING  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P1-02、P1-10  
**阻塞：** P1-14  
**Labels：** `phase-1`、`tracing`、`renderer`、`opentelemetry`

### 用户故事

作为 OpenTelemetry Runtime 的开发者，我需要一个稳定、明确标识为 Instrumentation Plan 的 `otel.yaml`，使 Runtime 能按 Root/Child Span、属性和错误规则实现插桩，而不会把它误认为 Collector 配置。

### 业务价值

- 形成可审查的 Trace 配置契约并锁定 Semantic Conventions 版本。
- 把 Span 生命周期、Context 和安全属性从语言实现中解耦。

### In Scope

- Span Plan 到 typed YAML model 的转换和 canonical 渲染。
- Root/Child、五种 Span Kind、parent、attributes、status、events 的表达。
- 渲染后严格校验和 Golden tests。

### Out of Scope

- OpenTelemetry Collector receivers/processors/exporters 配置。
- SDK 初始化、Propagator、Sampler 和 BatchSpanProcessor 参数。
- Backend endpoint 或 credential。

### 实现任务

1. 提供 `RenderTracing(plan, policy) ([]byte, error)` 或等价纯函数接口。
2. Header 的 `document_type` 固定为 `instrumentation.tracing`，并包含固定 `semantic_conventions_version`。
3. 在文档顶层显式写明 `plan_kind: instrumentation` 或等价强类型字段，防止消费者当作 Collector config。
4. 将 Root/Child Span 按 Plan ID 排序；attributes 按 semantic key 排序；events 按稳定 event ID 排序。
5. 完整渲染 target、kind、name、lifecycle、parent strategy、context carrier、attributes、status mapping 和 events。
6. Runtime Binding 使用 source/path/type/required/fallback 明确表达，不渲染伪运行时值。
7. 对 Resource attributes 和 Span attributes 分组，避免 `service.name` 在每个 Span 中重复定义且语义不一致。
8. 不输出 disabled internal spans、空 attribute、空 event 或 unspecified enum。
9. 禁止 Renderer 新增 sampling、exporter、endpoint 或凭据字段；Schema 也不得允许这些未知字段。
10. 渲染到内存后使用 P1-02 strict decoder 与 semantic validator 复验。
11. 验证所有 parent strategy 与 kind 组合合法，例如 `SERVER/extract_or_root`、Dependency/current_context。
12. Renderer 错误使用 `GEN_RENDER_ERROR` 并包含 Span ID，不返回部分 bytes。
13. 为全部五种 Span Kind、status mapping、exception event、空事件和 disabled internal call 建立 Golden YAML。
14. 编写字段语义文档，明确该文件不是 OpenTelemetry Collector 可直接启动的配置。

### 验收标准

#### AC1：Root 和 Child Span 完整渲染

- **Given** 一个包含 HTTP Root、SQL Client、Kafka Producer 和 Internal Span 的有效 Plan
- **When** 渲染 `otel.yaml`
- **Then** 每个 Span 各出现一次，kind、parent、lifecycle、attributes 和 target ID 与 Plan 一致

#### AC2：Semantic Conventions 版本固定

- **Given** 默认 tracing Policy
- **When** 渲染文件
- **Then** 输出包含一个具体支持版本，不包含 `latest`、版本范围或运行时查询结果

#### AC3：不是 Collector 配置

- **Given** 渲染后的文档
- **When** 检查顶层字段
- **Then** `document_type` 明确为 instrumentation plan，且不存在 receivers、processors、exporters、service.pipelines

#### AC4：非法 Parent 组合被拒绝

- **Given** Dependency Span 使用 `new_root` 或 Root Span 使用悬空 static parent ID
- **When** 渲染并校验
- **Then** 返回 `GEN_RENDER_ERROR`，不返回部分 YAML

#### AC5：敏感属性无回流

- **Given** 已净化 Plan 和含 canary 的原始 IR
- **When** Renderer 只接收 Plan 并渲染
- **Then** 输出不包含 raw URL、SQL、Redis key、payload 或 canary

#### AC6：输出确定

- **Given** 同一 Plan
- **When** 在不同目录和时区渲染 10 次
- **Then** bytes 与 SHA-256 完全一致

### 测试要求

- Unit tests：nil/invalid Plan、kind、parent、binding、events。
- Golden tests：完整 tracing 文件和最小 tracing 文件。
- Contract tests：Go Validator 与机器 Schema 双重通过。
- Security tests：canary 扫描和 forbidden Collector fields。
- Benchmark：1,000 SpanPlan 的纯渲染时间和 allocations。

### 非功能要求

- Renderer 不访问文件系统、环境、时钟或网络。
- 复杂度不高于 $O(N \log N)$。

### Story DoD

- Renderer、Schema contract tests、Golden files 和使用说明完成。
- OTel Reviewer 确认 kind、status、parent 和 semantic version 表达。
- 所有 AC 自动化。

---

## P1-12 生成结构化 Logging Plan 和关联脱敏策略

**Issue Type：** Story  
**Epic：** P1-LOGGING  
**优先级：** High  
**建议工作量：** 5 SP  
**依赖：** P1-05  
**阻塞：** P1-13  
**Labels：** `phase-1`、`logging`、`correlation`、`redaction`

### 用户故事

作为服务维护者，我需要 Endpoint 和 Dependency 的结构化日志事件自动包含 request/trace/span 关联和统一上下文，同时默认排除敏感、高基数内容。

### 业务价值

- 让用户可以从日志跳转到 Trace，并按 service/module/function/operation/status 查询。
- 统一成功、失败和生命周期事件，减少自由文本日志。
- 在 Runtime 实现前固定不可放宽的 credential 保护边界。

### In Scope

- HTTP、gRPC、Cron 的完成/失败事件，可选开始事件。
- 六种 Dependency 的默认失败事件。
- request ID、trace ID、span ID、service、module、function、operation、status、version 等字段 Binding。
- Severity、trigger condition、redaction 和字段限制。

### Out of Scope

- 日志 backend、encoder、rotation、retention 或传输配置。
- 自动记录请求/响应 body、SQL、Redis value、消息 payload 或完整 error message。
- 为每个成功 Dependency 调用默认生成日志，避免日志量失控。

### 默认事件矩阵

| 实体 | 条件 | Event Name | Severity | 默认启用 |
| --- | --- | --- | --- | --- |
| HTTP | start | `http.request.started` | `INFO` | 否 |
| HTTP | end + ok | `http.request.completed` | `INFO` | 是 |
| HTTP | end + error/timeout/cancelled | `http.request.failed` | `ERROR` 或受控映射 | 是 |
| gRPC | start | `rpc.request.started` | `INFO` | 否 |
| gRPC | end + ok | `rpc.request.completed` | `INFO` | 是 |
| gRPC | end + error/timeout/cancelled | `rpc.request.failed` | `ERROR` 或受控映射 | 是 |
| Cron | start | `cron.job.started` | `INFO` | 否 |
| Cron | end + ok | `cron.job.completed` | `INFO` | 是 |
| Cron | end + error/timeout/cancelled | `cron.job.failed` | `ERROR` 或受控映射 | 是 |
| Dependency | end + error/timeout/cancelled | `dependency.operation.failed` | `ERROR` 或受控映射 | 是 |

Severity 默认映射：`ok -> INFO`、`cancelled -> WARN`、`timeout/error -> ERROR`、`unknown -> WARN`。Event condition 必须互斥，失败时不得同时发出 completed 和 failed。

### 实现任务

1. 建立 EndpointKind 和 DependencyKind 到 event family 的 exhaustive mapping。
2. 为每个事件生成稳定 ID、event name、target ID/type、trigger、condition 和 severity mapping。
3. start event 仅在 `emit_start_events=true` 时生成；不应包含 duration 或 result status。
4. completion event 只匹配 `status=ok`；failed event 匹配 error/timeout/cancelled/unknown 的文档化集合。
5. Dependency 默认只生成 failed event；不得为每次成功调用增加默认日志。
6. 建立公共字段 Binding：
    - `timestamp` -> runtime clock，由 Runtime 写入，Generator 不填实际时间
    - `event.name` -> Plan constant
    - `service`、`module`、`function`、`operation` -> IR constants
    - `version` -> runtime resource，缺失为 `unknown`
    - `status`、`duration_seconds` -> runtime result，只在适用 trigger 中出现
7. 建立 correlation Binding：
    - `request_id` -> runtime request context，可选，不得自动生成伪 ID
    - `trace_id`、`span_id` -> current span context，存在有效 Span 时 required
8. HTTP 允许添加 method 和 route template；gRPC 允许 service/method；Cron 允许稳定 job name；Dependency 允许 system/operation。
9. 默认 error 字段只允许 `error.type` 和受控 error category；`error.message`、stacktrace、wrapped value 默认不绑定。
10. 对无 Root Span 的 Dependency 调用，trace/span bindings 标记为 optional；不得填空字符串冒充有效 ID。
11. 应用不可关闭的 redaction policy：credential 名称、header/cookie、payload/body、query、SQL、key/value 和 PII 类字段。
12. 字段 key 在忽略大小写及 `-`/`_` 后不得重复；重复时失败并指出 LogPlan ID。
13. 限制每个 event 字段数量、字段名长度和常量长度；超过上限返回 Diagnostic，不静默截断。
14. 所有常量在进入 Plan 前通过敏感值与 cardinality policy；Diagnostic 不回显被拒绝值。
15. 保持 field、condition 和 redaction rule 的确定排序。
16. 文档化 Runtime 对 required/optional/fallback correlation field 的责任。

### 验收标准

#### AC1：Endpoint 完成与失败互斥

- **Given** 一个 HTTP Endpoint 的 ok、error、timeout、cancelled runtime 结果
- **When** 评估生成的 event conditions
- **Then** ok 只匹配 completed，其余各只匹配 failed，不发生双重记录

#### AC2：默认不生成开始事件

- **Given** 默认 Policy
- **When** 规划 HTTP、gRPC、Cron logs
- **Then** 只包含 completed/failed；启用 `emit_start_events` 后每个 Endpoint 增加一个 started event

#### AC3：关联字段来源明确

- **Given** 一个 HTTP Root Span 下的完成事件
- **When** 检查 LogPlan
- **Then** request_id、trace_id、span_id 都是 runtime context Binding，不是静态值或 Generator 生成 ID

#### AC4：无 Trace Context 不伪造 ID

- **Given** 一个没有可证明 Root Span context 的 Dependency failed event
- **When** 规划 Logging
- **Then** trace_id/span_id 为 optional binding，缺失时省略，不输出 `unknown` 或随机值

#### AC5：敏感字段不可进入 Plan

- **Given** IR 和测试配置中包含 authorization、cookie、password、SQL、Redis value、payload canary
- **When** 规划 Logging
- **Then** fields、constants、diagnostics 中均不存在 canary，且产生可定位但不回显值的安全 Diagnostic

#### AC6：默认日志量受控

- **Given** 六种 Dependency 各一个成功和失败结果
- **When** 使用默认 Policy
- **Then** 只有失败结果匹配日志事件；成功调用不生成 dependency completion log

#### AC7：Severity 映射完整

- **Given** 五种受控 status
- **When** 评估 severity rule
- **Then** 每种 status 有唯一结果且未知字符串不能绕过 enum 校验

### 测试要求

- Event condition truth-table tests。
- Endpoint/Dependency mapping tests 和 unknown kind negative test。
- Correlation required/optional/fallback tests。
- Duplicate normalized field tests。
- Canary credential/PII/payload tests，扫描 Plan 和 Diagnostic bytes。
- Policy matrix：start/completion/dependency errors/correlation fields。
- Golden composite LogPlan。

### 非功能与安全要求

- 默认每个 Endpoint 最多两个事件；启用 start 后最多三个；每个 Dependency 默认最多一个事件。
- 不提供关闭内置 credential denylist 的 API。
- 不把 source location、绝对路径或源码片段作为日志字段。

### Story DoD

- 全部事件族、condition、severity、correlation、redaction 和 tests 完成。
- Security/SRE review 通过。
- 所有 AC 自动化。

---

## P1-13 渲染并校验 `logging.yaml`

**Issue Type：** Story  
**Epic：** P1-LOGGING  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P1-02、P1-12  
**阻塞：** P1-14  
**Labels：** `phase-1`、`logging`、`renderer`、`yaml`

### 用户故事

作为 Runtime 和日志规范评审者，我需要一个稳定、严格校验且不包含运行时真实数据的 `logging.yaml`，使结构化日志可以按相同字段和脱敏规则实现。

### 业务价值

- 将日志 schema、事件条件和关联字段变成机器可消费契约。
- 在文件落盘前验证所有 required binding 和不可放宽 redaction rule。

### In Scope

- Log Plan 到 typed YAML model 的转换和 canonical 渲染。
- Event、severity/condition、field binding、correlation、redaction 的完整表达。
- 渲染后严格校验、安全扫描和 Golden tests。

### Out of Scope

- 实际日志行、当前时间、真实 request/trace/span ID。
- Logger backend、格式化库或文件 rotation。
- 文件系统写入。

### 实现任务

1. 提供 `RenderLogging(plan, policy) ([]byte, error)` 或等价纯函数接口。
2. Header 的 `document_type` 固定为 `instrumentation.logging`。
3. 顶层输出不可变内置 redaction rules 和用户可追加规则；内置规则必须标记为不可关闭。
4. Events 按 Plan ID 排序，fields 按 key 排序，conditions 按有限 status 顺序输出。
5. 完整渲染 target、trigger、condition、severity mapping、field type、binding source、required 和 fallback。
6. 明确 request_id/trace_id/span_id 为 runtime context source；不得在文件中生成示例真实 ID。
7. timestamp 只表达 runtime binding，禁止写入生成时刻。
8. Renderer 不自行增加 error.message、payload、headers、SQL 或 key fields。
9. 渲染到内存后使用 strict decoder、semantic validator 和 canary scanner 复验。
10. 校验 failed/completed condition 互斥，field key 无规范化冲突，所有 required redaction rule 存在。
11. Renderer 错误返回 `GEN_RENDER_ERROR` 和 LogPlan ID，不返回部分 bytes。
12. 为 Endpoint events、Dependency errors、start on/off、optional correlation 和 immutable redaction 建立 Golden YAML。
13. 文档化 Runtime 对 optional field 省略语义，不允许输出空字符串占位。

### 验收标准

#### AC1：完整 Logging 文件有效

- **Given** 包含 Endpoint completed/failed 和 Dependency failed 的有效 Plan
- **When** 渲染 `logging.yaml`
- **Then** 通过 Go Validator 和机器 Schema，所有 event/field binding 与 Plan 一致

#### AC2：不含真实运行时值

- **Given** 任意 Plan
- **When** 渲染
- **Then** 文件不包含生成 timestamp、随机 request ID、trace ID、span ID 或主机信息

#### AC3：内置脱敏规则存在且不可关闭

- **Given** 默认 Policy
- **When** 渲染并解析
- **Then** credential denylist 完整存在并标记 immutable，用户规则只能追加或收紧

#### AC4：非法条件被拒绝

- **Given** completed 与 failed conditions 同时匹配 `error`
- **When** 渲染后校验
- **Then** 返回 `GEN_RENDER_ERROR` 且无部分 bytes

#### AC5：Canary 不泄漏

- **Given** 原始 IR 含 secret/PII/payload canary
- **When** 渲染已规划日志
- **Then** YAML 和 error message 中均不存在 canary

#### AC6：输出确定

- **Given** 同一 Plan
- **When** 在不同时区、目录和环境变量下渲染
- **Then** bytes 完全一致

### 测试要求

- Unit tests：nil/invalid Plan、conditions、severity、fields、redaction。
- Golden tests：默认完整文件、启用 start、最小文件。
- Contract tests：Go Validator 与机器 Schema。
- Security canary tests。
- Benchmark：1,000 LogPlan 的纯渲染时间和 allocations。

### 非功能要求

- Renderer 无 I/O 和全局可变状态。
- 输出使用 LF，编码为 UTF-8，文件内容不带 BOM。

### Story DoD

- Renderer、Golden files、Schema tests 和 Runtime 字段说明完成。
- 所有 AC 自动化。

---

## P1-14 实现 `si generate` 和安全文件提交

**Issue Type：** Story  
**Epic：** P1-CLI  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P1-08、P1-11、P1-13  
**阻塞：** P1-15  
**Labels：** `phase-1`、`cli`、`filesystem`、`offline`

### 用户故事

作为本地开发者，我需要通过一个安全、离线的命令扫描项目并生成所选观测文件，同时能够预览、严格校验并保护已有文件。

### 业务价值

- 将 Phase 1 能力提供为可直接使用和自动化的 CLI 工作流。
- 通过预渲染、校验、覆盖保护和原子替换降低工作区损坏风险。

### 命令契约

```text
si generate [path]
   --config <si.yaml>
   --output-dir <directory>
   --signals metrics,tracing,logging
   --include <patterns>
   --exclude <patterns>
   --include-tests
   --strict
   --dry-run
   --force
   --format text|json
```

- 默认 path 为当前目录。
- 默认 output dir 为 `<source-root>/generate`。
- 默认生成三类 signal。
- `--format` 控制命令报告，不改变 YAML 文件格式。
- `--dry-run` 完成扫描、规划、渲染和校验，但不创建目录或文件。

### In Scope

- Cobra command、参数与现有 scan/config pipeline 复用。
- Analyze -> Plan -> Render -> Validate -> Commit 编排。
- 输出预检、symlink/类型保护、原子单文件替换、失败清理和结构化报告。

### Out of Scope

- 自动执行生成文件、安装 SDK 或连接后端。
- 删除未选 signal 或输出目录中的其他文件。
- Watch mode 和后台 daemon。

### 实现任务

1. 在根命令注册 `generate [path]`；path、include/exclude/include-tests 的语义与 `scan` 一致。
2. 抽取 scan request 构建与 Analyzer transport 生命周期，避免复制配置解析和错误映射；不得改变 `scan` 公开行为。
3. 注册命令契约中的 flags；使用 optional 值区分“未设置”和显式 false/空值。
4. 合并 defaults、YAML、CLI 为已验证 Policy；参数错误使用 exit `2` 和 `CLI_INVALID_ARGUMENT`。
5. 执行完整 pipeline，并在每阶段检查 context cancellation。
6. Analyzer 返回 nil document、schema 不兼容、Plan fatal、Renderer/Validator 错误均使用 exit `1`，错误包含阶段但不泄漏配置值。
7. 根据 signals 只调用相应 Renderer；未选择的文件不创建、不覆盖、不删除。
8. 所有选中 Renderer 必须先在内存中成功并复验；任一失败时不得开始文件写入。
9. 解析 output dir：
    - 相对路径基于 source root
    - 清理 `.`/`..` 后得到明确路径
    - 拒绝 NUL、目标为普通文件、选中目标为 symlink 或非 regular file
10. 在写入前一次性检查所有选中目标；任一已存在且无 `--force` 时返回 `GEN_OUTPUT_EXISTS`，不写任何文件。
11. `--force` 只允许替换 `metrics.yaml`、`otel.yaml`、`logging.yaml` 中本次选中的文件；不得删除目录中的其他内容。
12. 每个文件在目标目录内写入唯一临时文件，使用 exclusive create、权限 `0644`、完整 write/close/sync 后再 rename。
13. 多文件提交 journal 记录每个目标提交前是否存在；已有目标保留同目录备份，新建目标记录 `previously_absent`。后续 rename 失败时恢复已替换目标、删除本次已创建目标，并返回原始错误与 rollback 状态。
14. 无论成功、失败或取消，都清理本次临时/备份文件；清理错误不得吞掉，应与主错误合并并保留上下文。
15. 对 transport/file close error 显式处理，不使用忽略返回值的 defer。
16. 防止并发生成互相覆盖：使用目标目录级 exclusive lock 或等价跨平台机制；锁冲突快速失败，不等待无限时间。
17. `--dry-run` 禁止任何 mkdir、lock、temp 或目标写入；文本报告列出计划文件、定义数量、Warning 数和 SHA-256。
18. 定义强类型、版本化 `GenerateReport`：report schema、status、CLI/IR/Generator schema versions、service、selected signals、completed stage、planned files、counts、diagnostics、dry_run、written 和可选 sanitized error；禁止输出 YAML raw content。
19. JSON 模式下，flags 解析成功后的成功与失败都先写一个完整 report，再通过标记为 `reported` 的 command error 保留进程退出码并阻止 runner 重复写 stderr。文本成功报告列出文件和数量，文本 Warning/错误写 stderr。
20. 保持完全离线：不解析或连接 exporter URL，不请求 Schema registry，不运行 Git 命令。
21. 增加 `si generate --version`，输出 CLI、IR Schema 和 Generator Schema version；不改变 `si scan --version` 的既有字段。
22. 更新 help 和 README，给出默认、signal subset、dry-run、strict、force 示例及覆盖风险说明。

文件提交只保证**单文件原子替换**和**进程内可处理错误的多文件回滚**。进程被强制终止、内核崩溃或断电时可能留下由完整旧/新文件组成的混合版本，但不会留下已 rename 目标的截断内容；该限制必须进入 README 和发布清单，不得宣称跨文件 crash-atomic。

### 验收标准

#### AC1：默认生成三个文件

- **Given** 一个有效 composite fixture 且目标目录不存在
- **When** 执行 `si generate <fixture>`
- **Then** exit `0`，创建三个可严格校验文件，报告定义数量正确

#### AC2：Dry Run 零写入

- **Given** 任意有效 fixture 和不存在的 output dir
- **When** 执行 `--dry-run`
- **Then** 完成所有内存校验并报告三个 hash，但 output dir 仍不存在

#### AC3：Signal subset 不影响其他文件

- **Given** output dir 中已有用户管理的 `otel.yaml` 和其他文件
- **When** 执行 `--signals metrics --force`
- **Then** 只写 `metrics.yaml`，其余文件 bytes 与 metadata 不变

#### AC4：默认拒绝覆盖

- **Given** 任一选中目标已存在
- **When** 不带 `--force` 执行
- **Then** exit `1`，返回 `GEN_OUTPUT_EXISTS`，所有目标 bytes 保持不变

#### AC5：Force 只替换受管目标

- **Given** 三个旧生成文件和一个 `notes.txt`
- **When** 带 `--force` 默认生成
- **Then** 三个目标被合法新文件替换，`notes.txt` 不变

#### AC6：渲染失败不写文件

- **Given** 一个使 Logging Renderer 校验失败的测试 Plan
- **When** 执行三 signal 生成
- **Then** exit `1`，Metrics/OTel/Logging 均未创建或修改

#### AC7：提交中途失败可恢复

- **Given** 两组场景：所有目标原先存在，以及所有目标原先不存在；FileWriter 均在第二个 rename 返回错误
- **When** 分别执行 `--force`
- **Then** 已存在目标恢复原 bytes，新建目标恢复为不存在，无截断文件，临时/备份被清理，错误包含 rollback 结果

#### AC8：Symlink 目标被拒绝

- **Given** `metrics.yaml` 是指向输出目录外的 symlink
- **When** 执行生成
- **Then** exit `1`，外部文件不变，命令报告不安全目标类型

#### AC9：Strict Warning 失败且不写入

- **Given** 动态 target 产生 Warning
- **When** 执行 `--strict`
- **Then** exit `1`，不创建 output dir 或目标文件

#### AC10：JSON 报告可机器解析

- **Given** flags 已成功解析后的成功、dry-run、配置错误、Planner Warning、Renderer 错误和 Writer 错误场景
- **When** 使用 `--format json`
- **Then** stdout 始终为一个符合 `GenerateReport` schema 的 JSON document，stderr 为空，report status/error 与进程退出码一致

#### AC11：Scan 无回归

- **Given** 全部 Phase 0 CLI tests 和 JSON snapshots
- **When** 增加 generate command 后重跑
- **Then** `si scan` 输出、退出码和文件副作用与 Phase 0 一致

#### AC12：离线执行

- **Given** `GOPROXY=off`、`GOSUMDB=off` 和阻断网络的测试环境
- **When** 执行 composite generate
- **Then** 命令成功且无外部连接尝试

### 测试要求

- CLI integration tests：默认、各 signal subset、flags precedence、dry-run、strict、force、JSON、invalid path/config。
- FileWriter fault injection：open/write/sync/close/rename/rollback/cleanup 各错误点，同时覆盖“原目标存在”和“原目标不存在”。
- Filesystem tests：existing directory/file、permissions、symlink、concurrent lock、path normalization。
- Existing Phase 0 CLI regression suite。
- Cross-platform tests 至少覆盖 macOS/Linux，Windows 路径与 rename 语义由 CI 验证。

### 非功能与安全要求

- 所有用户路径通过 `filepath` API 处理，禁止字符串拼接路径。
- 不跟随选中目标 symlink，不执行 shell，不加载输出目录中的代码。
- Writer 通过接口注入，业务 pipeline 可在无真实磁盘写入下测试。

### Story DoD

- 命令、帮助、README、Writer fault tests 和跨平台路径 tests 完成。
- 所有 AC 自动化。
- Phase 0 scan suite 无回归。

---

## P1-15 建立 Generator Fixture 与端到端 Golden 测试

**Issue Type：** Story  
**Epic：** P1-QUALITY  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P1-14  
**阻塞：** P1-16  
**Labels：** `phase-1`、`testing`、`fixtures`、`golden`

### 用户故事

作为维护者，我需要覆盖每种 IR 实体、信号、安全降级和文件行为的离线 Fixture，使规则或 Schema 变化能够在 PR 中以清晰差异被审查。

### 业务价值

- 防止新增规则破坏命名、字段、确定性或安全边界。
- 为 Phase 2 和 Runtime 提供可信的示例输入输出。

### In Scope

- Planner、Renderer、CLI 三层测试 Fixture 和 Golden files。
- 正向、边界、错误、安全、确定性和文件系统场景。
- Golden 更新流程和差异可读性。

### Out of Scope

- 连接真实 Prometheus、Collector 或日志 backend。
- 运行负载测试或验证真实 telemetry 上报。
- Phase 2 Dashboard fixtures。

### Fixture 组织建议

```text
testdata/
   generator/
      ir/
         composite.json
         dynamic-targets.json
         invalid-references.json
         naming-collisions.json
         sensitive-values.json
      golden/
         composite/
            metrics.yaml
            otel.yaml
            logging.yaml
         policy-summary-enabled/
         policy-internal-spans-enabled/
      cli/
         expected-report.json
```

端到端测试继续复用 `testdata/fixtures/*` 的真实源码扫描结果；直接 Planner tests 使用固定 IR，以便精确隔离 Generator 行为。

### 实现任务

1. 建立 composite IR，覆盖 HTTP、gRPC、Cron、六种 Dependency、resolved/unresolved CallEdge 和 source diagnostics。
2. 为 Phase 0 每个单项源码 fixture 生成三类预期输出或明确的“不适用 + Diagnostic”断言。
3. 建立动态目标 fixture：HTTP URL、RPC target、SQL、Redis、Kafka topic/group 均含 unknown 路径。
4. 建立 naming collision fixture，覆盖大小写、连字符、Unicode 归一化和相同 operation。
5. 建立 sensitive canary fixture，使用明显测试值覆盖 token、password、userinfo、query、SQL、Redis key/value、payload、email。
6. 建立 invalid IR fixtures：nil service 等价结构、重复 ID、悬空 function/target、unknown enum、unsupported schema。
7. 建立 policy fixtures：默认、Summary、internal spans、start logs、signal subset、strict。
8. 为三个 Renderer 保存 byte-for-byte Golden；同时严格解析后做语义断言，避免仅靠 snapshot 掩盖错误。
9. 为 CLI 保存稳定 JSON report Golden；路径字段使用 fixture 相对值，不含 temp 绝对路径。
10. 端到端执行 source scan -> plan -> render -> temp output，验证文件名、Schema、定义数量和 Diagnostic。
11. 对相同 fixture 连续运行 10 次，并对打乱 IR 顺序后的结果比较三文件 bytes。
12. 在不同 `TZ`、locale、工作目录和无关环境变量下重复确定性测试。
13. 建立无输出副作用测试：dry-run、strict failure、render failure、existing file、writer fault。
14. 建立 canary scanner，同时扫描 Plan protobuf JSON、三个 YAML、CLI stdout/stderr 和 error strings。
15. 建立 offline guard；测试期间禁用 module/network 访问，任何连接尝试导致失败。
16. Golden 更新必须通过显式命令或环境开关触发，默认 test 永不自动重写；更新后打印受影响 fixture 列表。
17. 在贡献文档中规定：新增 IR kind、attribute、config option 或 schema field 必须同时更新对应 fixture 和 traceability matrix。

### 最小测试矩阵

| 场景 | Plan | Metrics | Tracing | Logging | CLI |
| --- | --- | --- | --- | --- | --- |
| HTTP | 必须 | 必须 | 必须 | 必须 | E2E |
| gRPC | 必须 | 必须 | 必须 | 必须 | E2E |
| Cron | 必须 | 必须 | 必须 | 必须 | E2E |
| Kafka Producer/Consumer | 必须 | 必须 | 必须 | failed event | E2E |
| SQL/Redis | 必须 | 必须 | 必须 | failed event | E2E |
| HTTP/RPC Client | 必须 | 必须 | 必须 | failed event | E2E |
| Dynamic target | Warning | 降级 | 降级 | 安全字段 | strict/non-strict |
| Sensitive values | 阻止 | 无泄漏 | 无泄漏 | 无泄漏 | 无泄漏 |
| Invalid reference | Fatal | 不调用 | 不调用 | 不调用 | exit 1 |
| Signal subset | 对应 Plan | 选中 | 选中 | 选中 | 文件副作用 |

### 验收标准

#### AC1：能力矩阵全覆盖

- **Given** Phase 0 支持的所有 Endpoint/Dependency kind
- **When** 查询 test matrix 与自动化 test names
- **Then** 每种 kind 在 Plan 及适用的三个 signal 中至少有一个正向断言

#### AC2：Golden 差异稳定可读

- **Given** 无代码变化的同一 commit
- **When** 连续运行全套 Generator tests
- **Then** 无 Golden diff；失败时报告具体 fixture、文件和结构字段

#### AC3：输入排列不影响文件

- **Given** composite IR 的至少 20 个确定随机排列
- **When** 生成三文件
- **Then** 每个排列与基准 Golden bytes 相同

#### AC4：Canary 全链路不泄漏

- **Given** sensitive fixture 中每类 canary
- **When** 执行 Planner、Renderer 和 CLI
- **Then** 所有序列化输出和错误通道均无 canary

#### AC5：错误无文件副作用

- **Given** invalid reference、strict Warning、Renderer failure 和 rename failure
- **When** 分别执行 CLI
- **Then** 目标保持原状，无临时或备份文件残留

#### AC6：Golden 不会被测试隐式更新

- **Given** 故意改变一个预期字段但未设置 update 开关
- **When** 运行测试
- **Then** 测试失败并展示 diff，不修改 Golden 文件

#### AC7：离线通过

- **Given** 网络不可用和 module proxy 关闭
- **When** 运行全部 Generator tests
- **Then** 测试成功，无下载、backend 或外部进程依赖

### 测试要求

- 本 Story 本身通过 test inventory 检查测试矩阵完整性。
- Golden parser 必须给出语义 diff，不只报告 bytes mismatch。
- Temp directories 由测试框架管理并在成功/失败后清理。
- Fault injection 不依赖 chmod 才能触发，以保证 Windows 可测试。

### 非功能要求

- 单元测试和 Golden tests 可按 signal/fixture 单独运行。
- Fixture 小而单一；composite 仅用于跨 signal E2E。
- 测试无 sleep、无真实时钟比较、无随机 seed 漂移。

### Story DoD

- 测试矩阵、全部 fixtures、Goldens、更新流程和贡献说明完成。
- 所有 AC 自动化。
- QA 可仅按文档在离线环境重现通过和失败场景。

---

## P1-16 建立质量门禁、性能基线和发布检查

**Issue Type：** Story  
**Epic：** P1-QUALITY  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P1-15  
**阻塞：** Phase 1 Release  
**Labels：** `phase-1`、`ci`、`performance`、`release`

### 用户故事

作为项目维护者，我需要自动质量门禁和可由非作者执行的发布清单，使 Phase 1 的契约、安全、确定性和跨平台行为在发布前得到验证。

### 业务价值

- 阻止 Schema 漂移、Golden 未更新、敏感值泄漏和 CLI 文件回归进入主分支。
- 为后续 Dashboard/Runtime 开发提供可依赖的 `v0.2.0` 基线。

### In Scope

- CI jobs、Make targets、race/vet/test/schema/golden checks。
- Planner/Renderer 与 scan+generate 性能基线。
- 跨平台 build/test、文档、版本输出和 Phase 1 release checklist。

### Out of Scope

- 发布到具体 package manager 或自动签名/notarization。
- 生产后端容量测试。
- Phase 2 质量门禁。

### 实现任务

1. 增加明确任务入口，例如：
    - `make generator-test`
    - `make generator-contract-test`
    - `make generator-golden-test`
    - `make generator-race`
    - `make generator-perf`
    - `make phase1-quality`
2. `phase1-quality` 至少执行 Go tests、vet、Phase 0 fixture regression、Generator tests、race、Protobuf/Schema generation consistency、Golden consistency、security canary 和 performance budget。
3. CI 使用固定 Go/Protobuf 工具版本；依赖新增必须显式固定版本并通过 license/security review。
4. 增加 import boundary check，阻止 Generator packages 导入 Analyzer/AST。
5. 增加 schema compatibility job：Phase 0 fixtures 可读取；三个 v1 output schemas 可验证全部 Goldens；重新生成无 diff。
6. 增加 determinism job：同一 composite fixture 多次生成并比较 hashes。
7. 增加 race job，覆盖 Planner、Renderers、CLI orchestration 和并行只读 Plan 场景。
8. 建立 `BenchmarkGenerateFromIR`：预构造 IR，不包含 Analyzer 成本，分别记录 100/1,000/10,000 entities。
9. 建立 `BenchmarkScanAndGenerateComposite`：记录端到端时间和 allocations，与 Phase 0 scan 基线分开报告。
10. 首次发布建议预算：1,000 个预构造实体的 Plan + 三 Renderer 在参考 CI 上低于 1 秒且新增分配低于 64 MiB；若参考环境不稳定，使用固定基线的 20% 回归阈值并记录批准原因。
11. 端到端基线不得掩盖 Analyzer 成本；报告 scan、plan、render、write 四阶段耗时。
12. 在 macOS、Linux、Windows CI 至少执行 build、unit/contract tests 和 CLI temp-directory tests。
13. 更新 README：命令、配置、三个输出契约、Instrumentation Plan 边界、示例、退出码和已知限制。
14. 新建 `docs/phase-1-release-checklist.md`，包含 prerequisites、quality gates、offline generation、schema compatibility、determinism、security canary、cross-platform、performance 和 sign-off。
15. `si generate --version` 固定输出 CLI `v0.2.0`、IR schema 和 Generator schema；release build 可通过 ldflags 注入 CLI version，不允许 source hardcode credential。
16. 对 release artifact 执行一个真实 fixture 的 dry-run 和写入验证，并记录三个文件 hashes。
17. 将 Phase 1 quality job 配置为 Pull Request required check；任一子检查失败阻止合并。
18. 发布清单记录已知限制：不自动插桩、不生成 Collector 配置、不推断 Kafka Handler Root、不验证运行时 telemetry。

### 验收标准

#### AC1：单一质量入口完整

- **Given** 干净 checkout 和文档化工具版本
- **When** 执行 `make phase1-quality`
- **Then** 所有 Phase 0/1 required checks 执行并成功，任一失败返回非零

#### AC2：生成一致性无漂移

- **Given** 已提交 Proto、Schema 和 Golden artifacts
- **When** CI 重新生成
- **Then** Git working tree 无 diff

#### AC3：安全回归阻止合并

- **Given** 测试分支故意将 canary SQL/token 写入任一 YAML 或错误消息
- **When** 运行 quality job
- **Then** security test 失败且 required check 阻止合并

#### AC4：性能回归可解释

- **Given** 固定 CI runner 和 baseline artifact
- **When** benchmark 超过批准阈值
- **Then** job 失败并报告阶段、时间、allocations 和 baseline；只能通过有理由的 baseline review 更新

#### AC5：跨平台 CLI 通过

- **Given** macOS、Linux、Windows jobs
- **When** 构建并执行 temp fixture generate tests
- **Then** 文件名、LF 内容、exit code 和 Schema 结果一致

#### AC6：发布清单可由非作者执行

- **Given** 一名未参与实现的维护者和 release candidate binary
- **When** 按 Phase 1 checklist 执行
- **Then** 能独立完成所有检查、记录 hashes 并得出明确 pass/fail

#### AC7：版本信息完整

- **Given** release candidate
- **When** 执行 `si generate --version`
- **Then** 输出 CLI、IR 和 Generator 三个具体版本，且与生成文件 Header 一致

### 测试要求

- 对 Make/CI 任务做至少一次故意失败验证，确认错误不会被 shell pipeline 吞掉。
- Benchmark 使用固定 fixture 和 `-benchmem`，保存原始结果供趋势比较。
- Release checklist 在 RC 上完成一次 dry run，并由执行人签字。

### 非功能与安全要求

- CI 和发布检查不需要外部 observability backend。
- 质量脚本不得输出环境中的 token 或完整环境变量。
- 缓存仅用于依赖和编译，不缓存生成结果来绕过 determinism test。

### Story DoD

- required CI、Make targets、benchmark、README 和 Phase 1 release checklist 完成。
- 发布演练通过并记录结果。
- 所有 AC 自动化或在 checklist 中有可审计人工证据。

---

## 8. 输出文件目标示例

以下示例定义 Phase 1 v1 契约的目标形态。P1-02 实现时可以调整字段的排版，但不得降低强类型、来源可追溯、无运行时真实值、版本化和严格校验要求。任何字段名调整必须同步更新 Schema、Golden、README 和本节。

### 8.1 `metrics.yaml`

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
  - id: metric:endpoint:http-create-order:requests
    name: orders_api_create_order_http_requests_total
    type: counter
    unit: "{request}"
    description: Completed HTTP requests handled by api.CreateOrder.
    target:
      type: endpoint
      id: endpoint:http:create-order
    record:
      trigger: operation_end
      value:
        source: constant
        number: 1
    attributes:
      - key: service
        type: string
        binding:
          source: ir
          path: service.name
      - key: operation
        type: string
        binding:
          source: constant
          string: POST /orders/{id}
      - key: status
        type: string
        binding:
          source: runtime_result
          path: operation.status
          allowed_values: [ok, error, cancelled, timeout, unknown]
```

### 8.2 `otel.yaml`

```yaml
schema_version: generator.otel/v1
document_type: instrumentation.tracing
plan_kind: instrumentation
semantic_conventions_version: "1.37.0"
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
      path: service.version
      fallback: unknown
spans:
  - id: span:endpoint:http-create-order:root
    name: POST /orders/{id}
    kind: server
    target:
      type: endpoint
      id: endpoint:http:create-order
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
          source: ir
          path: endpoint.http_method
      - key: http.route
        type: string
        binding:
          source: ir
          path: endpoint.http_path
      - key: code.function.name
        type: string
        binding:
          source: ir
          path: function.qualified_name
    events:
      - id: event:endpoint:http-create-order:exception
        name: exception
        condition: status_is_error
        attributes:
          - key: exception.type
            type: string
            binding:
              source: runtime_result
              path: error.type
```

### 8.3 `logging.yaml`

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
   -
      id: log:endpoint:http-create-order:completed
      event_name: http.request.completed
      target:
         type: endpoint
         id: endpoint:http:create-order
      trigger: operation_end
      condition:
         status_in: [ok]
      severity:
         constant: info
      fields:
         -
            key: timestamp
            type: timestamp
            binding:
               source: runtime_clock
               path: now
         -
            key: request_id
            type: string
            required: false
            binding:
               source: runtime_context
               path: request.id
         -
            key: trace_id
            type: string
            required: true
            binding:
               source: runtime_context
               path: trace.id
         -
            key: span_id
            type: string
            required: true
            binding:
               source: runtime_context
               path: span.id
         -
            key: service
            type: string
            required: true
            binding:
               source: ir
               path: service.name
         -
            key: module
            type: string
            required: true
            binding:
               source: ir
               path: function.package_path
         -
            key: function
            type: string
            required: true
            binding:
               source: ir
               path: function.qualified_name
         -
            key: operation
            type: string
            required: true
            binding:
               source: constant
               string: POST /orders/{id}
         -
            key: status
            type: string
            required: true
            binding:
               source: runtime_result
               path: operation.status
         -
            key: duration_seconds
            type: number
            required: true
            binding:
               source: runtime_result
               path: operation.duration_seconds
         -
            key: version
            type: string
            required: true
            binding:
               source: runtime_resource
               path: service.version
               fallback: unknown
```

## 9. Roadmap 需求追踪矩阵

| Phase 1 Roadmap 能力 | 负责 Story | 主要验收证据 |
| --- | --- | --- |
| `metrics.yaml` | P1-02、P1-08、P1-14 | P1-08 AC1/AC2，P1-14 AC1 |
| Counter | P1-01、P1-06、P1-07、P1-08 | P1-06 AC1，P1-07 AC6 |
| Histogram | P1-06、P1-07、P1-08 | P1-06 AC1，P1-07 AC1 |
| Gauge | P1-06、P1-07、P1-08 | P1-07 AC5/AC6 |
| Summary | P1-03、P1-06、P1-07、P1-08 | P1-06 AC4，P1-07 AC6 |
| `service/module/function/operation/status/version` | P1-04、P1-06、P1-07、P1-12 | P1-04 AC1，P1-06 AC1-AC3，P1-12 AC3 |
| `otel.yaml` | P1-02、P1-11、P1-14 | P1-11 AC1-AC3，P1-14 AC1 |
| Root Span | P1-09、P1-11 | P1-09 AC1-AC4 |
| Child Span | P1-10、P1-11 | P1-10 AC1/AC2 |
| Attributes | P1-04、P1-09、P1-10 | P1-09 AC5，P1-10 AC6 |
| Error/Status | P1-10、P1-11 | P1-10 AC5/AC7/AC8 |
| Events | P1-10、P1-11 | P1-10 AC5/AC8 |
| `logging.yaml` | P1-02、P1-13、P1-14 | P1-13 AC1，P1-14 AC1 |
| `request_id` | P1-12、P1-13 | P1-12 AC3/AC4 |
| `trace_id/span_id` | P1-12、P1-13 | P1-12 AC3/AC4 |
| 结构化 `service/module/version` | P1-12、P1-13 | P1-12 AC3，P1-13 AC1 |
| 确定性生成 | P1-01、P1-02、P1-05、P1-15 | P1-05 AC5，P1-15 AC2/AC3 |
| 敏感数据和基数保护 | P1-04、P1-07、P1-10、P1-12、P1-15 | P1-04 AC3-AC5，P1-15 AC4 |
| 离线 CLI | P1-14、P1-15、P1-16 | P1-14 AC12，P1-15 AC7 |

追踪规则：任何 Roadmap 能力如果没有至少一个负责 Story 和一个自动化验收证据，不得标记 Phase 1 完成。

## 10. Phase 级验收用例

| ID | 场景 | 输入/命令 | 预期结果 |
| --- | --- | --- | --- |
| P1-AT-01 | 默认端到端生成 | composite source + `si generate` | exit 0，三个文件存在且 Schema 有效 |
| P1-AT-02 | 重复生成确定性 | 同输入、同配置连续 10 次 dry-run/force | 三个 SHA-256 均不变化 |
| P1-AT-03 | Metrics 类型完整 | 开启 Summary 的 composite IR | 四种 Metric type 至少各一个且单位合法 |
| P1-AT-04 | Root Span 完整 | HTTP、gRPC、Cron | 三类 Root kind/name/parent 正确 |
| P1-AT-05 | Dependency Span 完整 | 六种 Dependency | Producer/Consumer/Client mapping 正确 |
| P1-AT-06 | Logging 关联 | HTTP completed event | request/trace/span binding 来源正确 |
| P1-AT-07 | 动态目标非 strict | dynamic-targets fixture | exit 0，安全降级并报告 Warning |
| P1-AT-08 | 动态目标 strict | 同上 + `--strict` | exit 1，零文件修改 |
| P1-AT-09 | 敏感值保护 | sensitive-values fixture | Plan、YAML、stdout、stderr 均无 canary |
| P1-AT-10 | 覆盖保护 | 已有三个目标且无 `--force` | exit 1，原 bytes 不变 |
| P1-AT-11 | Force 与无关文件 | 已有目标、notes.txt + `--force` | 仅受管文件变化 |
| P1-AT-12 | Dry Run | 不存在 output dir + `--dry-run` | 返回 counts/hashes，文件系统零变化 |
| P1-AT-13 | Writer 故障恢复 | 原目标存在/不存在两组场景，第二次 rename 注入失败 | 原目标恢复或新目标删除，无 temp/backup 残留 |
| P1-AT-14 | Schema 不兼容 | unsupported IR schema | exit 1，`GEN_UNSUPPORTED_SCHEMA`，零写入 |
| P1-AT-15 | Phase 0 回归 | 全部 Phase 0 tests | `si scan` 行为完全通过 |
| P1-AT-16 | 离线执行 | 禁用网络和 module proxy | generate 仍成功，无连接尝试 |
| P1-AT-17 | 跨平台 | macOS/Linux/Windows CI | YAML bytes、exit code、Schema 语义一致 |

## 11. 风险登记与决策门

| ID | 风险 | 概率 | 影响 | 缓解措施 | 决策门/Owner |
| --- | --- | --- | --- | --- | --- |
| R1 | 用户把 Plan 误认为运行时已插桩 | 中 | 高 | 文件明确 `instrumentation` 类型；CLI/README 强调边界 | P1-02 Schema Review / Product |
| R2 | 当前 IR 无法证明 Kafka handler root | 高 | 中 | 不猜测；仅生成 consumer 调用点 Span；记录限制 | P1-09 Review / Compiler Lead |
| R3 | OpenTelemetry Semantic Conventions 演进导致字段漂移 | 中 | 高 | 固定具体版本；升级必须独立兼容 Story | P1-09/P1-11 / SRE |
| R4 | Summary 增加成本且不可跨实例聚合 | 中 | 中 | 默认关闭；文档说明与 Histogram 的权衡 | P1-03 / SRE |
| R5 | 规范化名称碰撞导致配置不稳定 | 中 | 中 | 稳定 ID hash 消歧、Warning、strict fail | P1-04 / Generator Lead |
| R6 | IR 静态值包含秘密或 PII | 中 | 高 | denylist、allowlist、净化、canary 全链路测试 | P1-04/P1-15 / Security |
| R7 | 可处理错误可回滚，但进程崩溃/断电无法保证跨文件 crash-atomic | 低 | 高 | 全部预渲染；单文件原子 rename；存在性 journal + rollback；公开限制 | P1-14 / CLI Owner |
| R8 | 4 周内 62 SP 超出团队实际速度 | 中 | 高 | 仅在历史 velocity 与至少 4 人并行容量满足时采用；否则调整为 6 至 8 周；不牺牲 P1-15/16 | Sprint Planning / EM |
| R9 | 三套 YAML Schema 与 Proto 重复演进 | 中 | 中 | Plan 为语义源；contract tests 保证映射；版本独立 | P1-01/P1-02 / Architect |
| R10 | Vendor-neutral 输出无法直接部署 | 高 | 中 | 明确 Phase 1 目标；后端 adapter/runtime 另立 Story | Release Review / Product |

以下决策在 Week 1 结束前必须冻结，否则 P1-06 至 P1-13 不得进入 Done：

1. Generation Plan Protobuf 字段和版本策略。
2. 三个 YAML 的 Header、target、binding 和 enum 表达。
3. 确认 OpenTelemetry Semantic Conventions 固定为 `1.37.0`；任何调整同步修改 Policy、Schema、示例和 Golden。
4. Metric 命名、unit、bucket、Summary 默认值。
5. 内置 credential denylist 和允许的 correlation fields。
6. `si generate` 默认目录、overwrite、strict 和 dry-run 语义。

## 12. Jira 建单与流转规则

### 12.1 公共 Jira 字段

创建实际 Jira Issue 时，所有 Story 使用以下公共字段：

| Jira 字段 | 值/规则 |
| --- | --- |
| Issue Type | Story |
| Fix Version | `v0.2.0` |
| Parent | 本文对应 Epic |
| Components | `IR`、`Generator Core`、`Metrics`、`Tracing`、`Logging`、`CLI`、`Quality` 之一 |
| Priority | 使用各 Story 标注值 |
| Story Points | 使用建议值，Sprint Planning 可重新估算并记录理由 |
| Labels | 保留 `phase-1` 和 Story 中 signal/quality 标签 |
| Assignee | 进入 Sprint 前必须指定；Draft Backlog 可为空 |
| Sprint | 仅在满足 Definition of Ready 后填写 |
| Linked Issues | 使用 `blocks/is blocked by` 建立第 6.2 节依赖 |
| Acceptance Evidence | PR、自动化 test name、CI run 或 release checklist 记录 |

### 12.2 Sub-task 拆分规则

- 每个“实现任务”可以在需要不同 Owner、超过一天或可独立评审时建立 Sub-task。
- 推荐至少区分 `Implementation`、`Unit/Contract Tests`、`Documentation/Schema`；小 Story 可作为同一 PR checklist，不强制制造空 Sub-task。
- Sub-task 不得新增父 Story 未声明的产品范围。
- QA 验收不得只写“测试通过”；必须关联具体 AC 编号和证据。
- Story 有未完成 AC、未关闭高风险 Diagnostic 或 Golden 未审查时不得关闭。

### 12.3 状态建议

```text
Backlog -> Ready -> In Progress -> In Review -> In QA -> Done
```

- `Ready`：满足 Definition of Ready。
- `In Review`：实现、测试和文档均已提交，不是仅提交生产代码。
- `In QA`：所有自动门禁通过，等待 AC/安全/跨平台证据确认。
- `Done`：Story DoD、全局 DoD 和相关 AC 全部满足。

## 13. Phase 1 Definition of Done

### 13.1 功能

- [ ] `si generate [path]` 默认生成三个目标文件。
- [ ] Counter、Histogram、Gauge、Summary 均在契约和测试中受支持，Summary 默认关闭。
- [ ] HTTP、gRPC、Cron Root Span 和六类 Dependency Child Span 映射完成。
- [ ] Attributes、Status、Error Events 和可选 Internal Spans 完成。
- [ ] Logging 包含 request/trace/span 关联、服务上下文、互斥完成/失败事件和脱敏规则。
- [ ] 每种 Phase 0 实体均有输出或稳定 Diagnostic。

### 13.2 契约与兼容

- [ ] Generation Plan Proto 通过旧 IR compatibility tests。
- [ ] 三个 YAML 有版本化机器 Schema、严格 Go Validator 和公开示例。
- [ ] Plan/YAML 不含 AST、语言特有节点、实际生成时刻、真实 runtime 值、随机值或绝对路径；允许声明 `runtime_clock` 等 Binding。
- [ ] `si scan` 的 JSON、文本、退出码、离线行为和文件副作用无回归。
- [ ] CLI、IR、Generator Schema version 在 release artifact 中一致且可查询。

### 13.3 确定性与质量

- [ ] 同一输入、Policy、CLI 版本连续 10 次生成 bytes 一致。
- [ ] 打乱 IR slice 顺序不改变三文件。
- [ ] Unit、Contract、Golden、E2E、race、vet、schema 和 import boundary tests 通过。
- [ ] macOS、Linux、Windows required jobs 通过。
- [ ] 性能不超过批准预算，或有经审查的 baseline 变更记录。

### 13.4 安全与可靠性

- [ ] Canary secret/PII 在 Plan、YAML、stdout、stderr 和 error 中均不存在。
- [ ] Metrics series 估算不超过 Policy 上限；Dependency target/value/resource 不进入三类 signal；Trace/Logging 只使用 allowlist。
- [ ] `--dry-run` 零文件副作用，默认不覆盖，symlink 目标被拒绝。
- [ ] 可处理的写入错误不会留下截断或部分提交文件，fault injection 覆盖原目标存在/不存在的 rollback 与 cleanup；crash-atomic 限制已公开。
- [ ] 生成和测试完全离线，不执行 shell、不连接 backend、不读取凭据。

### 13.5 文档与发布

- [ ] README 说明命令、配置、输出、退出码、示例和能力边界。
- [ ] `docs/phase-1-release-checklist.md` 已由非作者完成一次 RC 演练。
- [ ] 所有 Story 关联 AC 证据、PR 和 CI run。
- [ ] R1 至 R10 均有明确处理状态，没有未接受的高影响风险。

### 13.6 最终业务验收

- **Given** 一个包含 HTTP、gRPC、Cron、Kafka、SQL、Redis、HTTP Client 和 RPC Client 的本地 Go 项目，且网络不可用
- **When** 用户使用默认 Policy 执行 `si generate <path>`
- **Then** 命令在不修改源码的情况下生成三个版本化、可严格校验、无敏感值、可重复的 Instrumentation Plan 文件
- **And** 每个已识别入口和依赖在适用 signal 中可追溯到原 IR stable ID
- **And** 所有无法安全推断的信息都通过稳定 Diagnostic 明确说明
- **And** 输出不宣称运行时 SDK、Collector 或 telemetry backend 已经配置或运行
