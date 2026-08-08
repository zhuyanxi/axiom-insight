# Phase 1 Release Checklist（v0.2.0）

本文档面向未参与实现的维护者；所有步骤可在离线环境独立执行。执行人在
Sign-off 一节签字。

## 0. Prerequisites

- Go `1.26.x`、`protoc`（含 `protoc-gen-go`）已安装。
- 干净 checkout：`git status` 为空，分支为 `main`。
- 网络仅用于首次依赖下载；此后全程离线。

## 1. Quality Gates

```sh
make phase1-quality
```

必须全部通过（任一失败非零退出）。`phase1-quality` 包含：build、vet、全部单元/
契约测试、Phase 0 fixture regression、Generator 测试、race、
Proto/Schema 重新生成一致性、Golden 一致性与 canary 测试。

## 2. Offline Generation

```sh
GOPROXY=off GOSUMDB=off make phase1-quality
```

离线模式下成功。

## 3. Schema Compatibility

```sh
go test ./generator/e2e/ -run 'Schema|Golden' -count=1
git diff --exit-code -- schemas/ testdata/generator/golden/
```

- Phase 0 fixtures 可读取（`go test ./ir/... ./plugins/...`）。
- 三个 v1 output schemas 校验全部 Goldens。
- 重新生成 Proto 无 diff（`make check-generated`）。

## 4. Determinism

```sh
go test ./generator/e2e/ -run 'Permutation|Determinism' -count=1
```

同一 composite fixture 多次生成 + 25 个排列，三文件 bytes 与 Golden 一致。

## 5. Security Canary

```sh
go test ./generator/e2e/ -run Canary -count=1
```

敏感 fixture 的 canary 不进入 Plan JSON、三个 YAML、CLI stdout/stderr 或错误消息。

## 6. Cross-Platform

在 macOS、Linux、Windows 各执行：

```sh
go build ./... && go test ./cmd/si-cli ./generator/... -count=1
```

文件名、LF 内容、exit code 与 Schema 结果一致。

## 7. Performance

```sh
SI_ENFORCE_PERF_BUDGET=1 go test ./generator/e2e/ -run TestPerformanceBudget1000Entities -count=1 -v
go test ./generator/e2e/ -run '^$$' -bench BenchmarkGenerateFromIR -benchmem -count=1 | tee perf-generate.log
go test ./cmd/si-cli -run '^$$' -bench BenchmarkScanAndGenerateComposite -benchmem -count=1 | tee perf-cli.log
```

预算（1,000 实体，Plan + 三 Renderer）：参考 runner 上 < 1 s、新增分配 < 64 MiB，
允许 20% 容差；超过必须由 baseline review 记录批准原因。端到端基准按
scan/plan/render/write 四阶段分别报告（对应微基准）。

## 8. Release Artifact 验证

```sh
go build -ldflags "-X main.cliVersion=v0.2.0" -o bin/si ./cmd/si-cli
bin/si generate --version
bin/si generate testdata/fixtures/http --output-dir /tmp/rc-verify --dry-run
bin/si generate testdata/fixtures/http --output-dir /tmp/rc-verify
shasum -a 256 /tmp/rc-verify/metrics.yaml /tmp/rc-verify/otel.yaml /tmp/rc-verify/logging.yaml
```

- `--version` 输出 CLI `v0.2.0`、IR schema `v1`、Generator schema `v0.2.0`，与文件
  Header 一致。
- dry-run 与写入验证均通过；记录三个文件 hashes 于下。

| 文件 | SHA-256 |
| --- | --- |
| metrics.yaml | |
| otel.yaml | |
| logging.yaml | |

## 9. 已知限制（必须随发布说明记录）

- 不修改用户源码、不自动插桩、不注入编译期代码。
- 输出文件是 Instrumentation Plan，不是 OpenTelemetry Collector 配置。
- 不推断 Kafka Handler Root Span。
- 不验证运行时 telemetry 是否真实产生。
- 文件提交保证单文件原子替换与进程内多文件回滚；跨文件 crash-atomic 不保证。

## 10. Sign-off

| 检查 | 执行人 | 结果（pass/fail） | 备注 |
| --- | --- | --- | --- |
| Quality gates | | | |
| Offline generation | | | |
| Schema compatibility | | | |
| Determinism | | | |
| Security canary | | | |
| Cross-platform | | | |
| Performance | | | |
| Release artifact + hashes | | | |
| 已知限制确认 | | | |
