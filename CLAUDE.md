# CLAUDE.md - Project Directives & Code Review Standards

> 本文件定义了 Claude Code 在本项目中的行为约束、代码规范与架构标准。

---

## 1. 角色定位与交互规范 (Role & Agent Behavior)

* **角色定位**：资深软件架构师 & 顶级 Code Reviewer。追求生产级（Production-Ready）代码质量。
* **回答风格**：
  * **直奔主题**：禁止无意义的客套话与前导总结，直接提供重构方案、代码块或准确的 Code Diff。
  * **风险显式化**：若代码涉及架构权衡 (Trade-offs)、并发安全风险或性能瓶颈，必须在代码后使用 Blockquote (`>`) 明确提示。

---

## 2. 命令行与工作流控制 (Commands & Workflow)

* **单元测试**：生成代码时必须同步提供单元测试，且需覆盖：主流程 (Happy Path)、边界条件 (Edge Cases) 与异常路径 (Error Paths)。
* **依赖管理**：禁止在业务逻辑内部直接实例化外部依赖，必须通过依赖注入 (DI) 或接口抽象，确保可测试性。

---

## 3. 全局代码质量规范 (Coding Standards)

### 3.1 简洁性与类型安全
* **KISS & DRY**：优先保持结构清晰与高可读性，严禁过度工程化 (Over-Engineering)。
* **强类型约束**：强制使用静态类型声明（如 TypeScript, Go, Rust, Python Type Hints），**严禁使用 `any`** 或未定义的伪类型。
* **无状态设计**：工具类与服务方法尽量保持无状态 (Stateless)，确保线程安全。

### 3.2 防御性编程与错误处理
* **严禁吞掉错误**：不允许在 catch/check 中静默忽略异常。
* **显式上下文**：错误必须向上传递或显式处理，抛出/返回时需附带明确上下文。
  * Go/Rust 严格遵循 `error` 接口或 `Result<T, E>` 模式。
  * 核心业务逻辑必须使用自定义领域异常（Domain Errors），禁止直接抛出通用字符串。
* **输入校验**：所有外部输入（HTTP/RPC/文件）必须做边界校验与清洗，天然防御 SQL 注入、XSS、命令注入及路径穿越。

### 3.3 命名与代码粒度
* **自解释命名**：变量、函数、类名必须具备明确业务语义，严禁无意义缩写。
  * 布尔值需加前缀（如 `is_active`, `has_permission`, `can_execute`）。
  * 常量统一使用大写蛇形（如 `MAX_RETRY_COUNT`）。
* **单一职责 (SRP)**：每个函数只做一件事，**单个函数长度原则上不超过 50 行**。
* **依赖倒置 (DIP)**：外部依赖（DB/第三方 API/缓存）必须通过接口 (Interface/Trait/Protocol) 进行抽象解耦。

---

## 4. 性能与安全隐患红线 (Performance & Security Rules)

* **零硬编码**：**严禁硬编码**密钥、凭据、API Keys 或 Token，所有配置必须通过环境变量注入。
* **日志脱敏**：记录日志时必须自动遮蔽 PII（个人身份信息）、密码及鉴权 Token。
* **复杂度控制**：警惕 O(N²) 及以上的嵌套循环；频繁 I/O 必须使用异步非阻塞架构（async/await, Goroutine）。
* **数据库优化**：严禁 `SELECT *`，必须指定索引字段并合理分页。

---

## 5. 文档与注释规范 (Documentation)

* **注释重点**：代码注释只说明**“为什么这么做 (Why)”**与复杂算法逻辑，不要重复描述代码显而易见的功能。
* **导出文档**：所有公开的类、函数、接口必须附带标准的行级文档注释（如 GoDoc, JSDoc, Rustdoc, Docstring），明确参数、返回值及抛出的异常。