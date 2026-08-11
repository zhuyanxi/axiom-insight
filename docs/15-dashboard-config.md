# Dashboard 配置契约（P2-03）

`si dashboard`（P2-11）通过现有 `si.yaml` 的可选 `dashboard` 节点选择低风险
Dashboard 表现参数。本文档定义该节点的字段、默认值、合并规则和无效示例；实现见
`dashboard` package 的 `Resolve` 与严格 YAML Decoder。本文档链接
[Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-03）。

## 合并优先级

```text
CLI flags > si.yaml > built-in defaults
```

- 未设置的 bool 与显式 `false` 严格区分：YAML 中显式 `false` 不会被默认值覆盖。
- 生成过程中不读取环境变量、Grafana 配置、当前工作目录、用户名或 Git 信息。
- `dashboard` 节点最大 1 MiB；未知字段、重复 key、anchor/alias、timestamp、
  非有限 float 均以 `DASHBOARD_INVALID_CONFIG` 拒绝，错误带 `dashboard.<field>`
  字段路径且不回显被拒绝的值。

## 默认值

零配置（si.yaml 不含 `dashboard` 节点）使用：

| 字段 | 默认值 |
| --- | --- |
| `output_dir` | `dashboards` |
| `title_suffix` | `Observability` |
| `datasource_variable_name` | `datasource` |
| `include_trace_links` | `true` |
| `include_client_dependencies` | `true` |
| `rate_interval` | `$__rate_interval` |
| `timezone` | `browser` |
| `refresh` | `30s` |
| `max_panels` | `200` |
| `max_queries` | `500` |
| `strict` | `false` |

## 字段约束

| 字段 | 约束 |
| --- | --- |
| `output_dir` | 非空、不含 NUL；相对路径基于 source root |
| `title_suffix` | 不超过 64 字符 |
| `datasource_variable_name` | 匹配 `[A-Za-z_][A-Za-z0-9_]{0,31}`；v1 仅允许 `datasource`，保留变量不可删除 |
| `include_trace_links` | bool；显式 `false` 关闭 trace deep link |
| `include_client_dependencies` | bool；显式 `false` 排除 HTTP/RPC Client item |
| `rate_interval` | v1 仅允许 `$__rate_interval` |
| `timezone` | 仅 `browser` 或 `utc` |
| `refresh` | 仅 `5s`、`10s`、`30s`、`1m`、`5m`、`15m`、`30m`、`1h`、`off` |
| `max_panels` | 有限正整数，硬上限 `1000`；超限必须失败，禁止截断 |
| `max_queries` | 有限正整数，硬上限 `5000`；超限必须失败，禁止截断 |
| `strict` | bool；显式 `false` 不提升 Warning |

## Policy Digest

`DashboardPolicy.Digest()` 使用 canonical JSON + SHA-256 计算配置指纹。
digest 排除 `output_dir`：同一语义配置在不同目录产生相同 digest；任意影响
Dashboard JSON 内容的有效字段改变都会改变 digest。

## 最小配置

```yaml
dashboard:
  output_dir: dashboards
```

## 完整配置

```yaml
dashboard:
  output_dir: dashboards
  title_suffix: Observability
  datasource_variable_name: datasource
  include_trace_links: true
  include_client_dependencies: true
  rate_interval: $__rate_interval
  timezone: browser
  refresh: 30s
  max_panels: 200
  max_queries: 500
  strict: false
```

## 显式关闭开关

```yaml
dashboard:
  include_trace_links: false
  include_client_dependencies: false
  strict: false
```

## 无效示例

以下配置必须失败并返回 `DASHBOARD_INVALID_CONFIG`，错误包含准确字段路径。

```yaml
dashboard:
  refresh: 45s
```

```yaml
dashboard:
  rate_interval: $__interval
```

```yaml
dashboard:
  unknown_field: true
```

```yaml
dashboard:
  datasource_variable_name: "$(datasource)"
```

```yaml
dashboard:
  max_panels: 0
```

```yaml
dashboard:
  max_queries: 99999
```

```yaml
dashboard:
  timezone: local
```

```yaml
dashboard:
  output_dir: "/tmp/out\x00"
```
