# Go 代码规范审查报告 — 命令行入口（第 6 步） — 2026-11-25

## 基本信息

- **审查范围**: `client/cmd/backup/main.go`，这一步新增的唯一文件（104 行）
- **Go 版本**: go1.26.3 windows/amd64
- **模块**: client、server（两个独立的 go.mod）
- **分支**: `feat/whalefall/cli/20261125`，从 master `f610408` 起的独立 worktree

## 工具检查结果

| 工具 | 结果 | 摘要 |
|------|------|------|
| gofmt -l | ✅ | 通过 |
| go vet ./... | ✅ | 两个模块都过。这一步在 worktree 里做，`frontend/dist/.gitkeep` 已经提交，embed 不再缺目录 |
| golangci-lint | ⏭️ | 未安装，跳过；项目 lint 配置里的 `revive` 规则这次仍然没有验证 |

## 规范审查结果

### 🛑 严重（必须修复）

无。

### ⚠️ 重要（建议修复）

无。

### 💡 建议（可选优化）

- **[设计取舍] 没有用 cobra**（`client/cmd/backup/main.go:30`）。ch23 说子命令式 CLI 用 cobra，这里的三个子命令都是固定位置参数，没有 flag、没有嵌套、也不需要补全，换 cobra 要给一个一百行的入口拉进一份依赖，还得让 `client` 主模块多背一层间接依赖。手写 switch 到这一步为止是划算的；等子命令要加选项（`--password` 之类）时再换，`main.go` 是唯一要改的地方。
- **[退出码] 通用错误和 `NotArchive` 撞在 1**（`client/cmd/backup/main.go:97`）。`errs.Code` 的注释说“数值会拿去当命令行退出码”，所以引擎错误按类别退 1–7。不是引擎错误时（比如归档路径打不开）也退 1，正好和 `NotArchive` 重合，脚本单看码分不出“不是归档”和“文件没打开”。通用的改 2 能分开，但标准 Go 工具的惯例是 1，先按惯例来，记在这。
- **[输出] 有文件被跳过时仍退 0**（`client/cmd/backup/main.go:68`）。第 5 步定的是“跳过不算致命错误”，清单打到 stderr，退出码保持 0。这跟 Duplicati 把这类问题报成 warning 是一个路子，但脚本拿它当门禁时要清楚：退 0 不代表所有文件都进了归档。真要严格可以以后加 `--strict`，这一步不加。
- **[待确认] 提示语中文、引擎错误英文**。CLI 自己的输出是中文，引擎的 `errs` 说法是英文（`InvalidArgument` → `invalid argument`），同一行里会混着出现，比如 `archive is inside the source directory: invalid argument`。接界面时统一文案一起定，现在不动。
- **[未验证] Ctrl+C 没有手测**（`client/cmd/backup/main.go:21`）。`signal.NotifyContext` 接上了 `os.Interrupt`，`Pack` 每写完一个文件查一次 `ctx`，所以打包中途取消会返回 `context canceled`、退 1、目标位置不留正式归档。这条在引擎侧第 5 步测过，样例目录太小，命令行这边按不停，没实际验证。
- **[无测试] 按计划，这一层不写自动化测试**，手跑一遍算过。下面的手测记录是验收依据。

## 需求符合性

- ✅ `client/cmd/backup/main.go`，`pack <源目录> <归档>`、`unpack <归档> <目标目录>`、`verify <归档>` 三个子命令
- ✅ 只有参数解析和调用引擎：`Scan`/`Pack`、`Unpack`、`Verify` 各调一次
- ✅ 出错时退出码非 0，错误信息打到 stderr；正常结果打到 stdout（`verify` 不存在的文件时 stdout 为空）
- ✅ 三个子命令各跑一遍样例目录，通过

## 手测记录

样例目录：`空 目录/`（空目录）、`a.txt`、`empty.txt`（空文件）、`sub/rand.bin`（3000 字节，跨多个 512 块）、`中文 名.txt`、`sub/deep/d.txt`，共 8 个节点。归档放在样例目录外面。

| 操作 | 结果 | 退出码 |
|---|---|---|
| `pack 样例 归档` | 写出归档，输出“已打包 8 个节点到 …，跳过 0 个” | 0 |
| `verify 归档` | 输出“校验通过” | 0 |
| `unpack 归档 目标` | 还原后 `diff -r` 与源目录一致 | 0 |
| `verify 截断的归档` | stderr: `a.txt: padding is incomplete: unexpected EOF` | 3 |
| `unpack 归档 非空目录` | stderr: `target directory is not empty` | 7 |
| `pack 源 源内路径` | stderr: `archive is inside the source directory: invalid argument` | 6 |
| `verify 不存在的文件` | stderr: `open …: The system cannot find the file specified.` | 1 |
| 无参数 / 未知子命令 | stderr: 提示 + 用法 | 1 |

跳过的文件这一层实测不了：`Scan` 和 `Pack` 紧接着执行，中间没法把文件删掉，跳过路径由引擎测试覆盖。

## deslop 结果 — 2026-11-25

逐块过了一遍，删了 0 处、改了 1 处。改的是打包汇总行：原来拿 `len(entries)` 当已打包数，有文件被跳过时会多报，改成 `len(entries)-len(skips)`。

保留下来、也可能被问到的三处：

- `help` / `-h` / `--help`（`main.go:41`）：计划里没写。零参数时本来就要打用法，多这四行，省得用户敲 `backup help` 看到“未知子命令”。
- `signal.NotifyContext`（`main.go:21`）：计划没写，但不接信号的话 `ctx` 只能传 `context.Background()`，第 5 步做的取消能力在唯一的调用方手里闲着。
- `exitCode`（`main.go:97`）：把 `errs.Code` 映射成退出码，是 `errs.Code` 注释里定下的用法，不算临时加的功能。

验证：`gofmt -l`、`go vet ./cmd/...`、`go build ./...`、`go test ./internal/backup/ -count=1`（91.0%）全部通过。

## 总结

- 问题统计: 🛑 0 个 / ⚠️ 0 个 / 💡 6 个
- 总体评价: 入口层是平的，一个子命令一个函数，从头读到尾不用跳。六条建议都是记录取舍，没有要改的代码。

## Todo 清单

无必须修复项。
