# Dashboard 确定性命名与布局契约（P2-04）

同一服务的 Dashboard 在输入顺序、执行环境变化时必须产生逐字节相同的 UID、
Panel/Row ID、标题、refId 与 grid 位置。本文档定义这些确定性规则；实现为
`dashboard` package 的 pure functions（`ids.go`、`names.go`、`layout.go`），
不访问 clock、hostname、filesystem、global RNG 或环境变量。本文档链接
[Phase 2 Backlog](jira/phase-2-jira-stories.md)（P2-04）。

## UID

格式固定：

```text
si-<normalized-service>-v1
```

- `normalized-service`：仅 ASCII 小写字母、数字与单个 `-`；分隔符连续折叠；
  首尾 `-` 去除；长度截断到 34。
- 总长度恒在 `[8, 40]`；字符集为 Grafana UID 字符集 `[a-z0-9_-]`。
- 无可归一化字符（纯符号、空串、纯 Unicode）时以 `sha256(service.name)[:16]`
  作为服务段；过短部分用同一 16-hex 前缀补齐。
- 不同原始名可归一化到同一 UID（如 `a.b` 与 `a_b`）；集合级消歧见下。

## Panel / Row ID

- 输入 key 为 `PanelIDKey(category, itemID, purpose)` / `RowIDKey(category,
  purpose)`。
- `ResolvePanelIDs`：FNV-1a 64 位哈希（`si-hash-v1`）掩码到正整数
  `[0, 2^31)`；key 完全排序后分配，冲突以显式 attempt 计数重哈希；
  结果按输入顺序对齐，与输入排列无关。
- hash 不是安全边界：所有输入先经 provenance 与字符 allowlist 校验。

## refId

- 每个 Panel 的 canonical query key 排序后依次分配 `A` 至 `Z`；
  结果与输入顺序对齐。
- 超过 26 个 query 必须失败（`DASHBOARD_PANEL_LIMIT_EXCEEDED`，
  `panel.ref_ids`），绝不产生 `AA` 或未定义 refId。

## 标题

- `ServiceTitle`：服务名取 basename（最后一个 `/` 或 `\` 段），去除控制字符，
  上限 64 字符；无字母数字时回退 SHA-256 前缀。
- `CategoryTitle`：受控映射，仅五个 v1 分类有标题。
- `PanelTitle`：仅由受控 category、operation 与 Phase 1 normalized name
  组合，空格连接，上限 64 字符；禁止自由模板。
- 控制字符、零宽格式字符（`unicode.Cf`）与路径结构永不进入标题。

## Collision 消歧

| 场景 | 规则 | 结果 |
| --- | --- | --- |
| 两个不同服务名归一化到同一 UID | 按名排序，首个保留，其余追加 `-<sha256(name)[:8]>` | `DASHBOARD_NAME_COLLISION`，UID 仍限 `[8, 40]` |
| 两个 item 标题相同 | 按 TargetID 排序，首个保留，其余追加 ` - <sha256(targetID)[:8]>` | `DASHBOARD_NAME_COLLISION`，标题仍限 64 |
| 完全相同的服务名 | 同一实体，共享同一 UID | 无诊断 |

## 布局

- 固定 24 列；stat 宽 6、timeseries 宽 12、table 宽 24。
- category 按固定顺序自上而下：`service_overview`、`http`、`rpc`、`kafka`、
  `database`、`cache`。
- 每个 category 一个 row panel（宽 24、高 1），其 panels 按 ID 升序
  在其下方换行排列；空 category 整体省略（无空 row、无网格洞）。
- 所有 placement 满足 `0 <= X`、`X+W <= 24`、`1 <= W,H <= 24`，
  同 category 内非 row panel 互不重叠。
- 复杂度 `O(N log N)`，排序（`ResolvePanelIDs`、refId、标题、grid）
  是唯一允许的对数步骤。

## 可测性

- AC1：同一 Catalog 的 25 个固定排列输出逐字节相同。
- AC2：冲突名产生唯一、长度合规的 UID/Panel ID 与 `DASHBOARD_NAME_COLLISION`，
  不回显原始敏感值。
- AC3：仅 HTTP item 的 Catalog 只生成 Overview 与 HTTP 两行。
- AC4：27 个 query 的 Panel 分配 refId 必须失败。
- Golden：`dashboard/testdata/deterministic_ids_golden.json` 固定 UID、IDs、
  refIds、标题与 grid；`SI_UPDATE_GOLDEN=1` 重新生成，重跑无 diff。
- hash 输入、`HashVersion`（`si-hash-v1`）与输出均由测试固定。
