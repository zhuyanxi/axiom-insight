# Code-driven Observability Platform Roadmap

> **Vision**
>
> 从 **Source Code** 自动构建完整的 Observability Stack，让开发者不需要手工编写 Metrics、Tracing、Logging、Dashboard、Alert、Runbook，最终实现 **Code → Runtime → AI** 的闭环。

---

# 总体演进路线

```text
                         Phase 0
                     Observability IR
                            │
                            ▼
                 Phase 1  CLI MVP
                            │
                            ▼
              Phase 2  Dashboard Generator
                            │
                            ▼
            Phase 3  CI/CD & Observability Linter
                            │
                            ▼
                  Phase 4  IDE Integration
                            │
                            ▼
         Phase 5  Runtime Integration(ServiceInsight)
                            │
                            ▼
               Phase 6  AI Observability Agent
                            │
                            ▼
               Phase 7  Enterprise Platform
```

---

# Phase 0 —— Compiler Foundation

预计：4~6 周

目标：

建立整个产品最核心的基础设施。

## Deliverables

### AST Parser

支持：

* Go AST
* Package Analysis
* Type Analysis
* Call Graph

识别：

```
HTTP Handler

gRPC

Cron Job

Kafka Consumer

Kafka Producer

SQL

Redis

HTTP Client

RPC Client
```

---

### Observability IR

建立统一中间表示。

例如：

```yaml
Service:
    name: order

Functions:

    CreateOrder:

        inputs:

            HTTP

        outputs:

            Kafka

        dependencies:

            Redis
            MySQL

        metrics:

            latency
            errors

        trace:

            root span

        logging:

            structured
```

这是整个项目最重要的数据结构。

---

### Plugin Framework

未来支持：

```
Go

Rust

Java

Python

Node

C#
```

统一输出 IR。

---

## MVP

```
si scan .
```

输出：

```
Found:

HTTP Handler: 38

gRPC: 21

Cron: 6

Redis: 18

SQL: 42
```

---

# Phase 1 —— Observability Generator

预计：4 周

目标：

真正开始自动生成可观测性配置。

## 自动生成

```
Metrics

Tracing

Logging
```

例如：

```
generate/

metrics.yaml

otel.yaml

logging.yaml
```

---

### Metrics

自动生成：

```
Counter

Histogram

Gauge

Summary
```

命名统一：

```
service

module

function

operation

status

version
```

---

### Tracing

自动生成：

```
Root Span

Child Span

Attributes

Error

Events
```

---

### Logging

自动生成：

```
request_id

trace_id

span_id

service

module

version
```

---

# Phase 2 —— Dashboard Generator

预计：4 周

目标：

生成 Grafana Dashboard。

输入：

```
Observability IR
```

输出：

```
dashboard.json
```

自动生成：

```
HTTP Dashboard

RPC Dashboard

Kafka Dashboard

Database Dashboard

Cache Dashboard
```

未来：

支持 Dashboard Template。

---

# Phase 3 —— SLO & Alert Generator

预计：3~4 周

自动生成：

```
SLO

SLI

Alert Rule
```

例如：

```
Availability

Latency

Error Rate

Saturation
```

输出：

```
alerts.yaml

slo.yaml
```

支持：

```
Prometheus

Grafana Alert

AlertManager
```

---

# Phase 4 —— Runbook Generator

预计：3 周

AI 自动生成：

```
Runbook.md
```

例如：

```
CreateOrder

↓

Latency

↓

Possible Causes

Redis

MySQL

Kafka

Network

↓

Mitigation

↓

Commands

↓

Dashboard Links
```

最终：

```
docs/

runbooks/
```

---

# Phase 5 —— CI/CD

预计：4 周

支持：

GitHub Actions

GitLab

Jenkins

Azure DevOps

PR 自动分析：

```
新增 Handler

↓

新增 Metrics

↓

新增 Dashboard

↓

新增 Alert
```

例如：

```
Observability Score

89%

Missing Metrics

2

Missing Trace

1

Missing Alert

3
```

形成：

> Observability Linter

---

# Phase 6 —— IDE Integration

预计：5 周

VSCode

JetBrains

支持：

Hover：

```
Latency

Errors

Trace

Dashboard
```

右键：

```
Generate Dashboard

Generate SLO

Generate Runbook

Open Trace
```

开发体验接近现代 AI Coding Assistant。

---

# Phase 7 —— Runtime Integration（ServiceInsight）

这是整个项目真正开始形成竞争壁垒。

Compiler 输出：

```
Observability IR
```

Runtime 收集：

```
Metrics

Trace

Logs

Events

Topology
```

形成：

```
Expected

↓

Actual

↓

Compare
```

例如：

Compiler：

```
应该调用：

Redis
```

Runtime：

```
没有调用 Redis
```

Compiler：

```
应该有：

CreateOrder()

↓

Inventory()
```

Runtime：

```
Inventory Timeout
```

Compiler：

```
应该有：

Latency <300ms
```

Runtime：

```
900ms
```

真正形成：

**Design-time + Runtime**

---

# Phase 8 —— AI Agent

这是未来最大的方向。

AI 可以理解：

```
Source Code

+

Observability IR

+

Runtime Metrics

+

Trace

+

Logs

+

Topology

+

Git Commit
```

用户：

```
为什么订单接口今天变慢？
```

Agent：

```
CreateOrder

↓

Inventory RPC

↓

Redis Timeout

↓

Release 7a2c31

↓

新增 Retry

↓

Latency +220ms
```

不仅回答原因，还能：

```
Generate Fix

Generate PR

Generate Runbook

Generate RCA
```

---

# 企业版（Enterprise）

未来企业版能力可以围绕治理和规模化展开。

| 能力                 | 描述                            |
| ------------------ | ----------------------------- |
| Multi-language     | Go、Rust、Java、Python 等统一 IR    |
| Multi-cluster      | 多 Kubernetes 集群               |
| Git Integration    | GitHub、GitLab、Azure DevOps    |
| Compliance         | 自动检查观测规范                      |
| Governance         | Dashboard 生命周期管理              |
| AI Knowledge       | 企业知识库与 Runbook 联动             |
| Change Impact      | Git Commit → Runtime 影响分析     |
| Cost Analysis      | Metrics、Logs、Trace 成本优化       |
| Policy Engine      | 观测策略与组织规范                     |
| Plugin Marketplace | 社区 Generator、Template、Rule 插件 |

---

# 最终产品架构（Vision）

```text
                           Source Code
                                │
                                ▼
                       Language Frontend
                  (Go / Rust / Java / Python)
                                │
                                ▼
                          AST + Semantic
                                │
                                ▼
                    Observability Compiler
                                │
                                ▼
                        Observability IR
                                │
        ┌───────────────┼────────────────┬────────────────┐
        │               │                │                │
        ▼               ▼                ▼                ▼
 Metrics Gen      Trace Gen        Log Gen        Topology Gen
        │               │                │                │
        └───────────────┼────────────────┴────────────────┘
                        ▼
               Dashboard / SLO / Alert / Runbook
                        │
────────────────────────┼────────────────────────────────────
                        ▼
               ServiceInsight Runtime Platform
                        │
        Metrics + Trace + Logs + Service Graph
                        │
                        ▼
               AI Observability Agent
                        │
                        ▼
      RCA / Change Analysis / Optimization / Auto Fix
```

## 建议的里程碑

如果以创业或产品孵化的节奏来规划，我建议采用 **12～18 个月** 的路线，而不是一次性实现全部功能。

| 时间      | 里程碑                           | 核心价值                |
| ------- | ----------------------------- | ------------------- |
| M1-M2   | Compiler Foundation（AST + IR） | 建立统一分析能力            |
| M3      | Metrics / Tracing Generator   | 实现最小可用价值            |
| M4      | Dashboard Generator           | 用户第一次看到“代码自动生成可观测性” |
| M5      | SLO / Alert Generator         | 完成观测配置自动化           |
| M6      | CI/CD + Observability Linter  | 融入研发流程              |
| M7-M9   | ServiceInsight Runtime 集成     | 打通设计时与运行时           |
| M10-M12 | AI Agent（RCA、Runbook、变更分析）    | 形成差异化竞争力            |
| M12+    | 多语言、插件生态、企业版                  | 构建平台能力              |

将 **Observability IR** 视为整个项目的核心资产。AST、Dashboard、AI Agent、ServiceInsight Runtime 都应围绕这套中间表示构建。这样无论未来增加新的语言支持、输出目标（例如 Datadog、New Relic）还是新的 AI 能力，都不需要重构整个系统，而是在 IR 之上增加新的前端或生成器即可。
