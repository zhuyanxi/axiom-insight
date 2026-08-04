# 统一命名、属性基数与隐私策略

本文档定义 Phase 1（P1-04）所有信号共享的命名规范、属性 allowlist、credential denylist、
名称碰撞策略和 series 预算估算。`generator/naming` 包是无状态策略层，Planner 在生成
GenerationPlan 前应用这些规则；任何被阻止的值只产生 Diagnostic，绝不进入 Plan。

## 1. 命名规范

### 1.1 机器名归一化

`NormalizeMachineName` 将 IR 稳定字段转换为 ASCII 机器名：

- 小写（Unicode 感知、与 locale 无关）
- 非字母数字字符序列折叠为单个 `_`，首尾 `_` 去除
- 不以 ASCII 字母开头时加 `m_` 前缀
- 单个组件最长 128 字节
- 无可使用字符时返回确定性错误（Planner 映射为 Diagnostic）

示例：`Checkout-Service API` → `checkout_service_api`；`2fa-service` → `m_2fa_service`；
`Order-Service-支付` → `order_service`。

### 1.2 Metric 名称

```
<namespace>_<service>_<module>_<function>_<operation>_<purpose>
```

- 全部组件归一化后以 `_` 连接，空组件省略
- 字符集 `^[a-z][a-z0-9_:]*$`，最长 200 字节（OpenMetrics 兼容）
- 超过长度限制时按确定性顺序丢弃最不具体的组件（先 function、再 module），
  仍超长则以 SHA-256 短后缀稳定截断；产生 `GEN_UNSUPPORTED_ENTITY` Warning

### 1.3 Span 名称

| 实体 | 规则 | 示例 |
| --- | --- | --- |
| HTTP | `<METHOD> <route-template>`；未知 method 用 `HTTP` | `POST /orders/{id}` |
| gRPC | `<service>/<method>` | `OrderService/CreateOrder` |
| Cron | `cron <稳定 job 名>` | `cron nightly_report_generator` |
| Dependency | `<system> <operation>`；缺失 operation 用 `unknown` | `sql exec` |

Dependency 名称不包含任何原始目标值（URL、host、key）。

### 1.4 Log Event 名称

小写点分段，如 `http.request.completed`、`dependency.operation.failed`、
`cron.job.started`。最长 200 字节。

## 2. 属性 allowlist

### 2.1 Metrics

默认 Attribute 仅 `service`、`operation`、`status`。Gauge 不允许 `status`。
`module`、`function` 只进入稳定名称与 target metadata，`version` 不进入 Metrics。

### 2.2 Tracing

正向 allowlist，未列出即拒绝：

- 通用：`service.name`、`service.version`、`operation`、`code.namespace`、`code.function`
- HTTP：`http.request.method`、`http.route`
- gRPC：`rpc.system`、`rpc.service`、`rpc.method`
- DB：`db.system`、`db.operation`
- Messaging：`messaging.system`、`messaging.operation`
- Cron：`cron.job.name`、`cron.job.schedule`

显式阻止（含前缀）：`url.full/scheme/host/path/query/fragment/userinfo`、
`http.url/target/query_string`、`http.request.header.*`、`http.request.cookie`、
`db.statement`、`db.redis.key`、`db.connection_string`、`messaging.destination*`、
`messaging.message.body/payload`、`server.address/port`、`net.peer.*`、`enduser.*`。
测试 URL `https://user:pass@example.com/orders?id=42#detail` 的全部组成部分均被拒绝。

### 2.3 Logging

字段 allowlist（受控 key）：`timestamp`、`event.name`、`service`、`module`、
`function`、`operation`、`status`、`version`、`duration_seconds`、`error.type`、
`error.category`、`request_id`、`trace_id`、`span_id`、`method`、`route`、
`rpc.service`、`rpc.method`、`cron.job.name`、`system`、`db.system`。

字段名比较忽略大小写，`-` 与 `_` 等价（`AUTH-TOKEN`、`auth_token` 归一化相同）。

## 3. Credential denylist（不可关闭）

内置 denylist：`authorization`、`cookie`、`password`、`secret`、`token`，扩展 PII 模式：
`email/mail`、`phone/mobile/tel`、`id_card/identity/passport/ssn`、`credit_card/card_number/cvv/pin`、
`api_key/apikey`、`session`、`auth`、`credential`、`private_key`、`passwd`。

- denylist 按归一化字段名（子串）匹配；用户 redact 配置只能增加条目，不能移除内置条目
- 敏感值（credential、PII、`sk-` 密钥、13+ 位数字、电话、邮箱、私钥头）被丢弃，
  只产生 `GEN_SENSITIVE_VALUE_DROPPED` Diagnostic，消息不含被移除的值
- 高基数值（URL、query、SQL、key 样式值）被丢弃，产生 `GEN_CARDINALITY_BLOCKED`

## 4. 名称碰撞策略

同一 signal 内归一化名称重复时：

1. 按 TargetID 字典序排序
2. 最小 TargetID 保留原名，其余追加 `_<sha256(TargetID)[:8]>` 后缀
3. 每个被加后缀的实体产生 `GEN_NAME_COLLISION` Warning（strict 模式失败）

映射只依赖碰撞组成员，与输入顺序无关。Hash 仅用于稳定消歧，不用于保护秘密。

## 5. Series 预算

classic-exposition 估算（每个 Attribute 组合计 1 条基数）：

| 类型 | 每个组合的 series 数 |
| --- | --- |
| Counter / Gauge | 1 |
| Histogram | `有限边界数 + 3`（有限 buckets、隐式 `+Inf`、sum、count） |
| Summary | `quantile 数 + 2`（quantiles、sum、count） |

超过 `max_instruments` 或 `max_estimated_series` 时返回
`GEN_CARDINALITY_LIMIT_EXCEEDED`，报告估算值、上限和 signal，绝不静默截断。

## 6. strict 行为

`GEN_NAME_COLLISION`、`GEN_CARDINALITY_BLOCKED`、`GEN_SENSITIVE_VALUE_DROPPED`、
`GEN_UNSUPPORTED_ENTITY`、`GEN_INCOMPLETE_TARGET` 为 Warning；strict 模式下提升为失败。
Error 级代码（`GEN_CARDINALITY_LIMIT_EXCEEDED` 等）任何模式都失败。

## 7. 安全性约束

- 名称、属性和诊断处理复杂度不高于 O(N log N)
- 不读取进程环境、Git 配置、文件系统或网络补全任何名称
- 被拒绝的值直接丢弃；Diagnostic 只含 TargetID、字段路径和规则说明
