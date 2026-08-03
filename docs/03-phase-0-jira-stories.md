# Phase 0 - Compiler Foundation Jira Stories

## 使用说明

**目标读者：** 中级和初级 Go 开发人员。

**Phase 0 目标：** 对本地 Go 项目执行 `si scan <path>`，分析包、类型和调用关系，识别 HTTP、gRPC、Cron、Kafka、SQL、Redis、HTTP Client、RPC Client，并输出稳定的 Observability IR 与摘要。

**不在范围内：** Metrics、Tracing、Logging 配置生成；Dashboard、SLO、Alert、Runbook；运行时数据采集；AI；其他语言的真实分析器。

**统一约束：**

- Go 实现；CLI 使用 Cobra，配置使用 Viper。
- 使用 `go/packages`、`go/ast`、`go/types`；不得自行实现 Go Parser。
- 编译链路固定为 `AST -> Semantic Model -> Observability IR`；Generator 不得直接读取 AST。
- IR 使用 Protocol Buffers；所有字段必须支持后向兼容演进。
- CLI 必须可离线运行，不能向外部服务发请求。
- 未来语言前端通过 gRPC/HashiCorp go-plugin 输出同一 IR；禁止 Go `plugin.so`。

## 建议 Epic 和依赖

| Epic | Story | 依赖 |
| --- | --- | --- |
| P0-FOUNDATION | P0-01 | 无 |
| P0-IR | P0-02, P0-03 | P0-01 |
| P0-GO-ANALYZER | P0-04 至 P0-09 | P0-02, P0-03 |
| P0-PLUGIN | P0-10 | P0-02 |
| P0-CLI | P0-11, P0-12 | P0-04 至 P0-10 |
| P0-QUALITY | P0-13 | P0-11, P0-12 |

---

## P0-01 建立 Go Monorepo Compiler 基础

**Epic：** P0-FOUNDATION  
**优先级：** Highest  
**建议工作量：** 2 SP

### 用户故事

作为 Compiler 开发者，我需要清晰的 Go 模块和包边界，使后续分析器、IR、CLI 和插件能独立开发和测试。

### 实现任务

1. 初始化根 `go.mod`，设定项目 module path 和最低支持 Go 版本。
2. 创建包目录：`cmd/si-cli`、`compiler/goanalyzer`、`compiler/semantic`、`ir`、`plugins`、`testdata`。
3. 添加 `Makefile` 或等价任务入口：`build`、`test`、`lint`、`generate`。
4. 配置 `.gitignore`，排除二进制、覆盖率和生成文件中不应提交的部分。
5. 编写根 `README.md` 的本地构建与测试命令，不描述未实现功能。

### 验收标准

- `go build ./...` 成功。
- `go test ./...` 成功。
- 每个顶层包只有一个明确职责，CLI 不包含 AST 分析逻辑。
- `testdata` 中的 fixture 可被测试读取，且不作为产品代码编译依赖。

---

## P0-02 定义版本化 Observability IR Protobuf Schema

**Epic：** P0-IR  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P0-01

### 用户故事

作为下游 Generator、Runtime 和 AI 的开发者，我需要语言无关且可演进的 IR 契约，使它们不依赖 Go AST。

### 实现任务

1. 创建 `ir/v1/observability.proto`，使用 `proto3` 和稳定 package 名称。
2. 定义 `ObservabilityDocument`，至少包含：`schema_version`、`service`、`functions`、`dependencies`、`diagnostics`。
3. 定义 `Service`：`name`、`source_root`、`language`、`packages`。
4. 定义 `Function`：稳定 `id`、名称、包路径、文件位置、输入端点、输出端点、依赖 ID、调用方/被调用方 ID。
5. 定义 `Dependency`：稳定 `id`、类型、名称、调用位置、所属函数、目标信息。
6. 使用 enum 表示端点和依赖种类，至少覆盖 HTTP Handler、gRPC Handler、Cron、Kafka Producer/Consumer、SQL、Redis、HTTP Client、RPC Client。
7. 定义 `Diagnostic`：severity、code、message、source location；解析失败不得静默丢失。
8. 配置 `buf` 或 `protoc` 生成 Go 代码；将生成方式写入 `generate` 任务。
9. 为关键消息增加 JSON 名称和字段注释；不得复用或改号已发布 field number。

### 验收标准

- `protoc`/`buf generate` 可重复生成 Go 类型，不产生未提交差异。
- 单元测试可构建一个包含 Service、Function、Dependency 的有效 document 并完成 Protobuf 序列化往返。
- JSON 输出使用稳定、可读字段名。
- Schema 中未出现 Go AST、`ast.Node` 或仅 Go 特有的内部类型。
- 新增字段遵守 append-only field number 规则。

---

## P0-03 建立 Semantic Model 到 IR 转换层

**Epic：** P0-IR  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P0-02

### 用户故事

作为 Go 分析器开发者，我需要独立的语义模型，使 AST 识别规则和 IR 表达可以分别变化。

### 实现任务

1. 在 `compiler/semantic` 定义纯 Go 领域模型：Service、Package、Function、Endpoint、Dependency、CallEdge、Diagnostic。
2. 为函数、依赖和调用边生成确定性 ID；同一源码输入重复扫描必须得到相同 ID。
3. 设计 `SourceLocation`，包含相对文件路径、起止行列；不得保存机器绝对路径到 IR。
4. 实现 `ToIR(document semantic.Document) (*observabilityv1.ObservabilityDocument, error)`。
5. 对缺少位置、未解析目标和未知依赖类型建立显式诊断，而不是 panic。
6. 使用 table-driven tests 覆盖空文档、单函数、多依赖、未知依赖、相同输入确定性。

### 验收标准

- AST 包不直接引用 Protobuf 生成类型；只创建 Semantic Model。
- IR 转换器不导入 `go/ast`、`go/types` 或 `go/packages`。
- 相同 semantic document 连续转换两次，序列化结果一致。
- 所有路径相对 source root；测试不依赖开发机用户名或目录。

---

## P0-04 加载 Go Package、AST 和类型信息

**Epic：** P0-GO-ANALYZER  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P0-02, P0-03

### 用户故事

作为用户，我需要分析器正确加载目标目录中的 Go 包和类型信息，使后续识别不依赖脆弱的字符串匹配。

### 实现任务

1. 实现 `Analyze(ctx, sourceRoot, options)` 入口，验证目录存在且可读。
2. 使用 `go/packages.Load` 加载目标根下包；请求 Name、Files、CompiledGoFiles、Syntax、Types、TypesInfo、Imports。
3. 排除 vendor、隐藏目录、生成的测试 fixture 外无关目录；是否包含 `_test.go` 由 options 显式控制，默认不包含。
4. 将 package loading error 转换为 Diagnostic；可用包继续分析，整体错误仅用于不能开始分析的情况。
5. 解析 Go module/service 名称：优先 `si.yaml` 显式值，其次 module name，最后根目录名。
6. 建立 source root 相对路径转换函数，并处理 macOS/Linux 路径分隔符。

### 验收标准

- 分析含多个 package 的 fixture 时，每个可加载 package 都进入 Semantic Model。
- 类型信息可用于识别外部函数的完整 package path。
- 错误 Go 文件产生 Diagnostic，CLI 不 panic。
- 没有网络时，对已存在本地模块可完成扫描。
- 单元测试覆盖无 `go.mod`、加载错误、多包、包含 test file 开关。

---

## P0-05 建立函数目录和静态调用图

**Epic：** P0-GO-ANALYZER  
**优先级：** High  
**建议工作量：** 5 SP  
**依赖：** P0-04

### 用户故事

作为后续依赖识别和拓扑功能的开发者，我需要可追溯的函数目录与调用关系。

### 实现任务

1. 遍历 `*ast.FuncDecl`，收集函数名、receiver、完整包路径、签名、位置和导出状态。
2. 处理普通函数、方法和匿名函数；匿名函数使用父函数加位置构成稳定 ID。
3. 遍历函数体中的 `*ast.CallExpr`，通过 `types.Info` 解析 callee object 和 package path。
4. 对项目内可解析调用创建 `CallEdge`；对动态调用、函数变量、interface dispatch 创建 `UNRESOLVED_CALL` Diagnostic。
5. 避免递归遍历导致无限循环；调用图只记录边，不递归展开。
6. 提供查询函数：按稳定 ID 查函数、取得入边和出边。

### 验收标准

- fixture 中 `A -> B -> C` 产生两条方向正确的调用边。
- 方法调用可关联到声明类型和包路径。
- 无法静态解析的调用不会中断扫描，并至少保留源位置诊断。
- 递归函数和相互递归函数扫描完成。
- 测试覆盖普通函数、方法、匿名函数、递归、interface 调用。

---

## P0-06 识别入口端点：HTTP、gRPC 和 Cron

**Epic：** P0-GO-ANALYZER  
**优先级：** High  
**建议工作量：** 8 SP  
**依赖：** P0-04, P0-05

### 用户故事

作为用户，我需要扫描器找出服务入口，使 IR 能标记请求、RPC 和定时任务的根函数。

### 实现任务

1. 定义可配置 Framework Adapter 接口：输入函数/调用语义，输出 Endpoint；核心分析器不硬编码每个框架细节。
2. 实现标准库 `net/http` 识别：`http.Handle`、`http.HandleFunc`、`ServeMux.Handle`、`ServeMux.HandleFunc`。
3. 实现常用 Go HTTP Router 的初始 Adapter；首批支持范围和版本写入文档及 fixture，未知 router 输出 Diagnostic，不可猜测识别。
4. 实现 gRPC 注册识别：发现 `Register*Server` 调用并关联 server implementation 的方法。
5. 实现 Cron 识别：发现已明确支持库的任务注册 API，并记录 schedule 字符串和回调函数。
6. 将入口类型、HTTP method/path、gRPC service/method、cron schedule 保存到 Semantic Model。
7. 为无法解析的 handler callback 输出诊断，保留注册调用位置。

### 验收标准

- 标准库 HTTP fixture 的 handler 数量和路径与预期一致。
- gRPC fixture 能标记至少一个注册的 server method。
- Cron fixture 能标记任务函数和 schedule。
- Endpoint 关联到正确 Function ID，不仅是函数名称字符串。
- 不支持框架不会误报为已识别 HTTP/gRPC/Cron。
- 每个支持 Adapter 均有正常、错误和边界 fixture。

---

## P0-07 识别消息、数据库和缓存依赖

**Epic：** P0-GO-ANALYZER  
**优先级：** High  
**建议工作量：** 8 SP  
**依赖：** P0-04, P0-05

### 用户故事

作为用户，我需要扫描器识别函数对 Kafka、SQL 和 Redis 的静态依赖，便于理解数据流和外部风险点。

### 实现任务

1. 为依赖识别建立规则接口，规则接收已解析的 call expression 和类型信息。
2. 实现 `database/sql` 规则，识别 Query、QueryContext、Exec、ExecContext、Prepare、Begin 等调用；记录 operation 和可获取的 SQL 文本。
3. 实现首批 Redis client 规则；根据方法 receiver 的完整类型和 package path 识别，不能仅按 `Get`、`Set` 名称匹配。
4. 实现首批 Kafka client 规则，区分 Producer 与 Consumer；记录静态 topic/consumer group，动态值标记为 unknown。
5. 将 Dependency 归属到发起调用的 Function，并关联调用位置。
6. 去重同一函数内相同位置的重复遍历结果；不同调用位置必须保留为不同 dependency instance。

### 验收标准

- SQL fixture 正确输出 SQL dependency，且函数归属正确。
- Redis fixture 不将用户自定义 `Get`/`Set` 误识别为 Redis。
- Kafka fixture 能区分 producer 与 consumer。
- 动态 SQL/topic 不 panic，输出 dependency 且标记值未知。
- 规则单元测试覆盖类型匹配、错误匹配、动态参数和多个调用点。

---

## P0-08 识别外部调用：HTTP Client 和 RPC Client

**Epic：** P0-GO-ANALYZER  
**优先级：** High  
**建议工作量：** 5 SP  
**依赖：** P0-04, P0-05

### 用户故事

作为用户，我需要知道函数调用了哪些外部服务，以建立初始服务依赖图。

### 实现任务

1. 识别标准库 `net/http` client：`http.Get`、`http.Post`、`Client.Do`、`Client.Get`、`Client.Post`。
2. 从 `http.NewRequest` 和相关变量赋值中尽力追踪静态 URL、method、host；无法静态求值时保存调用类型和源位置。
3. 识别 gRPC client 方法调用：基于生成 client interface/receiver 类型与 package path，而非方法名称。
4. 记录目标服务线索：host、URL、gRPC target、package path；不得伪造无法确定的目标。
5. 识别项目内 wrapper 时只记录当前可证明的调用边；wrapper 展开留给后续故事，不增加不可靠推断。

### 验收标准

- 标准库 HTTP fixture 正确识别 HTTP Client dependency 和静态 URL/method。
- gRPC client fixture 正确识别 RPC Client dependency。
- 动态 URL 和动态 gRPC target 仍产生 dependency，但对应字段明确为 unknown/empty。
- Server handler 注册不会被误识别为 HTTP Client。

---

## P0-09 汇总分析结果并生成稳定扫描统计

**Epic：** P0-GO-ANALYZER  
**优先级：** Medium  
**建议工作量：** 3 SP  
**依赖：** P0-06, P0-07, P0-08

### 用户故事

作为 CLI 用户，我需要简洁、准确的识别数量，以快速确认项目被正确扫描。

### 实现任务

1. 定义 `ScanSummary`，统计 HTTP Handler、gRPC、Cron、Kafka Consumer、Kafka Producer、SQL、Redis、HTTP Client、RPC Client、诊断数量。
2. 从 Semantic Model/IR 计算统计；禁止 CLI 重新遍历 AST 计数。
3. 固定统计项目顺序，未发现项显示 `0`。
4. 为每个识别种类建立 fixture 和断言；覆盖一个函数拥有多个依赖的情况。
5. 将 summary 放入 API 返回值或从 IR 派生，不将它设为唯一事实来源。

### 验收标准

- 指定 fixture 的统计与预期逐项一致。
- 同一输入多次运行，统计输出顺序和数值一致。
- 空 Go 项目成功输出所有 9 项且均为 `0`。

---

## P0-10 定义语言前端插件 gRPC 契约与 Go 内置实现

**Epic：** P0-PLUGIN  
**优先级：** Medium  
**建议工作量：** 5 SP  
**依赖：** P0-02

### 用户故事

作为平台开发者，我需要统一插件协议，使 Rust、Java、Python 等语言前端未来能输出相同 IR。

### 实现任务

1. 在 Protobuf 中定义 `LanguageAnalyzer` gRPC service，包含 `GetMetadata` 与 `Analyze`。
2. `GetMetadata` 返回 language、插件版本、支持的能力和 IR schema version range。
3. `Analyze` 接收 source root、include/exclude、配置；返回 IR document 与 diagnostics。
4. 实现 Go analyzer 的 in-process adapter，满足同一接口；CLI 默认使用它。
5. 建立 plugin transport 抽象，为未来 HashiCorp go-plugin/gRPC process 留出实现点；Phase 0 不要求启动外部子进程。
6. 在 README 或协议文档中说明 handshake、版本不兼容行为和错误代码。

### 验收标准

- Go 内置 analyzer 与未来外部插件使用相同业务接口和返回类型。
- schema version 不兼容时返回清晰错误，不执行不安全反序列化。
- 单元测试验证 metadata、成功 analyze、无效路径和版本不兼容。
- 代码未导入或使用 Go 原生 `plugin` 包。

---

## P0-11 实现离线 `si scan` CLI 命令

**Epic：** P0-CLI  
**优先级：** Highest  
**建议工作量：** 5 SP  
**依赖：** P0-04 至 P0-10

### 用户故事

作为本地开发者，我需要运行 `si scan <path>`，无需服务端即可得到项目的可观测性分析摘要。

### 实现任务

1. 用 Cobra 创建 `si` 根命令和 `scan [path]` 子命令；默认 path 为当前目录。
2. 用 Viper 读取可选 `si.yaml`；配置服务名、启用语言、include tests、支持的 framework adapter。
3. 验证 path、配置和 Go 项目可加载性；将用户错误输出到 stderr。
4. 默认输出人类可读 summary，格式包含所有 Phase 0 识别类型。
5. 支持 `--format json` 输出完整 IR 或稳定 JSON scan result，供自动化消费。
6. 支持 `--output <file>` 写出 JSON；不提供时不得修改工作区文件。
7. 设计退出码：`0` 成功、`1` 无法完成扫描、`2` 参数/配置错误；有非 fatal diagnostics 仍可为 `0`。
8. 确保命令不发送网络请求，也不要求服务端、数据库或容器环境。

### 验收标准

- `si scan .` 在有效 fixture 中成功，输出各类型计数。
- `si scan . --format json` 输出可解析 JSON，含 schema version、IR 或 scan result、diagnostics。
- 无效目录返回退出码 `2` 并有可理解错误。
- 包加载失败返回退出码 `1`，保留诊断。
- `--output` 创建指定文件；未传入时扫描不创建文件。
- CLI integration test 在无网络环境执行。

---

## P0-12 提供可维护的扫描 Fixture 和端到端测试

**Epic：** P0-CLI  
**优先级：** High  
**建议工作量：** 5 SP  
**依赖：** P0-11

### 用户故事

作为维护者，我需要小型、隔离的示例项目，防止识别规则变化时无意破坏已有能力。

### 实现任务

1. 为每种支持能力建立最小 fixture package：HTTP、gRPC、Cron、Kafka、SQL、Redis、HTTP Client、RPC Client。
2. fixture 依赖应最小化；需要第三方类型时使用明确版本或本地 stub，测试不得访问网络。
3. 为每个 fixture 建立 expected JSON/snapshot，记录函数、依赖、端点和统计。
4. 编写端到端测试：执行 CLI 或 command handler，比较 JSON 语义而不是依赖 map 遍历顺序。
5. 添加 malformed fixture，验证诊断和退出码。
6. 记录新增 framework/dependency rule 时必须新增 fixture 的贡献规范。

### 验收标准

- 测试可单独运行每个 fixture。
- 每个 Phase 0 识别类型至少有一个正向 fixture 和一个不应匹配的测试。
- Snapshot 差异清晰展示，不因绝对路径、时间戳或 map 顺序波动。
- 离线 CI 中端到端测试成功。

---

## P0-13 建立质量门禁、错误处理和 Phase 0 发布检查

**Epic：** P0-QUALITY  
**优先级：** High  
**建议工作量：** 3 SP  
**依赖：** P0-11, P0-12

### 用户故事

作为项目维护者，我需要自动质量门禁和明确发布标准，避免不稳定的扫描器进入后续 Generator 阶段。

### 实现任务

1. 在 CI 执行 `go test ./...`、`go vet ./...`、Protobuf 生成一致性检查和 CLI fixture 测试。
2. 增加 race detector 任务，覆盖分析器并发安全性；若分析器暂不并发，记录原因并禁止共享可变全局状态。
3. 为 CLI 错误、内部错误、解析诊断建立统一错误码和可检索 message code。
4. 创建 Phase 0 release checklist：跨平台构建、离线扫描、JSON 有效性、所有 fixture、schema compatibility。
5. 定义性能基线：小 fixture 扫描时间和内存上限；基线用于回归告警，不作为过早优化目标。
6. 发布最小 CLI 版本号和 `si scan --version` 输出，包含 IR schema version。

### 验收标准

- Pull request 中任一质量检查失败会阻止合并。
- 生成的 Protobuf 文件与 schema 一致，无手工漂移。
- 已知错误场景的退出码、Diagnostic code 和用户消息可由测试断言。
- Release checklist 可以由非作者按步骤完成。
- `si scan --version` 同时输出 CLI 版本和 IR schema version。

---

## Phase 0 Definition of Done

- 新建或修改识别规则时，包含单元测试和对应 fixture。
- 所有公共 IR 字段、枚举值、CLI flag 都有最小文档。
- 所有错误使用可行动消息；不得 `panic`、忽略 package load error 或吞掉 type-check error。
- 输出可重复：同一源码、配置、CLI 版本得到相同 IR ID、排序和 summary。
- 所有验收测试在本地离线运行通过。
- Phase 1 Generator 只能读取 IR，不可导入 `compiler/goanalyzer` 或 Go AST 包。