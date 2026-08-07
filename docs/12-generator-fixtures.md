# Generator Fixture 与 Golden 测试（P1-15）

本文档记录 Phase 1 Generator 的 fixture 组织、能力矩阵、Golden 更新流程和贡献规则。

## 1. Fixture 组织

```text
testdata/
   generator/
      ir/                      # 固定 IR（protojson），Planner/Renderer 直接消费
         composite.json        # HTTP/gRPC/Cron + 六种 Dependency + resolved/unresolved edge
         dynamic-targets.json  # 全部动态 target（URL/RPC/SQL/Redis/topic/group）
         naming-collisions.json# 大小写/连字符/Unicode 归一化 + 相同 operation
         sensitive-values.json # token/password/userinfo/query/SQL/key/payload/email canary
         invalid-references.json # 悬空 function/target 引用
      golden/
         composite/            # 默认 Policy 三文件 Golden
         policy-summary-enabled/
         policy-internal-spans-enabled/
         policy-start-logs/
      cli/
         expected-report.json  # si generate --format json 报告 Golden
   fixtures/                   # Phase 0 真实源码 fixture（E2E source scan）
```

端到端测试 `generator/e2e` 使用固定 IR 精确隔离 Generator 行为；CLI 测试复用
`testdata/fixtures/*` 的真实源码。

## 2. 能力矩阵（AC1）

| 场景 | Plan | Metrics | Tracing | Logging | CLI | 自动化位置 |
| --- | --- | --- | --- | --- | --- | --- |
| HTTP | 必须 | 必须 | 必须 | 必须 | E2E | `e2e: TestCapabilityMatrixAC1`、`cmd: TestGenerateDefaultAC1` |
| gRPC | 必须 | 必须 | 必须 | 必须 | E2E | 同上 |
| Cron | 必须 | 必须 | 必须 | 必须 | E2E | 同上 |
| Kafka Producer/Consumer | 必须 | 必须 | 必须 | failed event | E2E | 同上 |
| SQL/Redis | 必须 | 必须 | 必须 | failed event | E2E | 同上 |
| HTTP/RPC Client | 必须 | 必须 | 必须 | failed event | E2E | 同上 |
| Dynamic target | Warning | 降级 | 降级 | 安全字段 | strict/non-strict | `e2e: TestGoldenScenariosE2E`、`cmd: TestGenerateStrictFailureWritesNothingAC9` |
| Sensitive values | 阻止 | 无泄漏 | 无泄漏 | 无泄漏 | 无泄漏 | `e2e: TestCanaryFullChainAC4` |
| Invalid reference | Fatal | 不调用 | 不调用 | 不调用 | exit 1 | `e2e: TestInvalidReferenceFatal` |
| Signal subset | 对应 Plan | 选中 | 选中 | 选中 | 文件副作用 | `cmd: TestGenerateSignalSubsetAC3` |

## 3. 确定性

- `e2e: TestPermutationInvarianceAC3`：composite IR 的 25 个固定随机排列 → 三文件 bytes 与基准一致。
- `e2e: TestDeterminismAC2`：10 次运行 + `TZ`/`LC_ALL`/无关环境变量 → bytes 一致。
- Golden 文件本身是 `SI_UPDATE_GOLDEN=1` 生成的字节快照；普通测试永不重写。

## 4. Golden 更新流程

```sh
SI_UPDATE_GOLDEN=1 go test ./generator/e2e/ ./generator/ ./cmd/si-cli/ -count=1
```

- 更新后测试会打印受影响 fixture 列表（`updated golden ...`）。
- 更新必须在 PR 中与产生差异的代码变更一起审查；Golden 差异应逐字节或逐结构字段解释。
- 失败时 Golden parser 给出语义 diff（结构字段对比），不只报告 bytes mismatch。

## 5. 贡献规则

新增 IR kind、attribute、config option 或 schema field 时，必须同时：

1. 更新对应 IR fixture（`testdata/generator/ir/`）或新增 fixture；
2. 更新/新增 Golden（走 `SI_UPDATE_GOLDEN` 流程）并解释差异；
3. 更新本文档的能力矩阵（第 2 节）；
4. 确保 canary fixture 覆盖任何新增敏感值路径；
5. 运行 `go test ./...`、`go vet ./...` 与 `go test -race ./generator/... ./cmd/...`。

## 6. 离线保证

所有 Generator 测试离线运行：无网络、无 backend、无外部进程依赖（canary 全链路
测试构建 CLI 二进制除外——构建本身离线）。CI 环境使用 `GOPROXY=off`、`GOSUMDB=off`。
