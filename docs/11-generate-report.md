# `GenerateReport` 契约（`cli.generate_report/v1`）

`si generate --format json` 在 flags 解析成功后，无论成功、Warning、配置错误还是
pipeline 错误，都向 stdout 写入**且仅写入一个** `cli.generate_report/v1` JSON
document；stderr 保持为空。Cobra 在识别 `--format` 前发生的 flag 语法错误仍走
Phase 0 文本 stderr。

## 字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `schema_version` | string | 固定 `cli.generate_report/v1` |
| `status` | string | `success` 或 `failure` |
| `cli_version` | string | CLI 版本 |
| `ir_schema_version` | string | IR Schema 版本 |
| `generator_schema_version` | string | Generator Schema 版本 |
| `service` | string | 分析服务名；flags 阶段失败时省略 |
| `signals` | string[] | 本次选中的 signal |
| `completed_stage` | string | `flags` / `scan` / `render` / `validate` / `commit` |
| `planned_files` | object[] | `name`、`definitions`、`sha256`、`existed_before` |
| `dry_run` | bool | `--dry-run` 时 true |
| `written` | string[] | 成功写入的目标文件名 |
| `diagnostics` | object[] | 生成器 Diagnostic（不含敏感值） |
| `error` | object | `code`、`stage`、`message`；仅 failure 时出现 |

约束：

- 报告**从不包含 YAML 文件内容**或敏感值。
- `status` 与进程退出码一致：`success` → `0`；`failure` → `1`（或配置错误的 `2`）。
- 失败场景的 `error.message` 是脱敏的阶段错误摘要，不回显被拒绝的值。
- JSON 模式下外层 runner 不重复打印已报告错误。

## 示例（成功）

```json
{
  "schema_version": "cli.generate_report/v1",
  "status": "success",
  "cli_version": "v0.1.0",
  "ir_schema_version": "v1",
  "generator_schema_version": "v0.2.0",
  "service": "orders",
  "signals": ["metrics", "tracing", "logging"],
  "completed_stage": "commit",
  "planned_files": [
    {"name": "metrics.yaml", "definitions": 6, "sha256": "7696d6a68df53754e6ee471b87f0a2cf78288e3b7183e4062a0b446782c1788c", "existed_before": false},
    {"name": "otel.yaml", "definitions": 2, "sha256": "52df13e33ce0cc76777769f560b0afecf7041bb845b42d84b393b17301319433", "existed_before": false},
    {"name": "logging.yaml", "definitions": 7, "sha256": "d4f6c2a1b8e0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c", "existed_before": false}
  ],
  "dry_run": false,
  "written": ["metrics.yaml", "otel.yaml", "logging.yaml"]
}
```

## 示例（提交失败）

```json
{
  "schema_version": "cli.generate_report/v1",
  "status": "failure",
  "cli_version": "v0.1.0",
  "ir_schema_version": "v1",
  "generator_schema_version": "v0.2.0",
  "service": "orders",
  "signals": ["metrics", "tracing", "logging"],
  "completed_stage": "commit",
  "planned_files": [],
  "dry_run": false,
  "error": {
    "code": "CLI_GENERATE_ERROR",
    "stage": "commit",
    "message": "GEN_OUTPUT_EXISTS: commit: target \"metrics.yaml\" already exists; use --force to replace it"
  }
}
```
