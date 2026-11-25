---
name: tencent-go-guide
description: "Go 代码规范审查：基于《腾讯 GO 语言编程指南》（腾讯代码委员会 Go 分会）审查 Go 代码——先跑 gofmt/go vet/golangci-lint 工具链，再按指南 26 章规则深度审查命名、错误处理、并发、接口、测试。Use when reviewing Go code, 代码审查/规范检查, or fixing Go style violations."
---

<!-- argument-hint: [审查范围：文件/commit/范围，或具体问题主题] -->

# Go 代码规范审查（腾讯 GO 语言编程指南）

基于《腾讯 GO 语言编程指南》对 Go 代码做**工具检查 + 规范深度审查**，在对话里直接说明问题与修复清单，不生成文档。指南全文已拆解为本 skill 的 `chapters/`（26 章）与 `cheatsheet.md`（决策表 + 坏味道速查）。

## 审查工作流

**0. 范围** → **1. 工具检查** → **2. 规范审查** → **3. 结论** → **4. Todo 清单**

### 步骤 0: 确定审查范围
根据用户输入确定审查对象：
- "审查本次变更/最近提交" → `git diff` / `git diff <tag>..HEAD` 或 `git log -1 -p`
- 指定文件 / 指定目录 → 直接确定目标
- 未指明时询问；本项目常为 `server/` 与 `client/` 两个独立 Go 模块

### 步骤 1: 工具检查（必做，先于人工审查）
```bash
python3 scripts/go_check.py                    # 整个仓库（自动发现 go.mod 模块）
python3 scripts/go_check.py --files a.go b.go  # 只查指定文件
python3 scripts/go_check.py --repo server/     # 只查某个模块
```
脚本聚合三步（详见 `scripts/go_check.py`）：
1. **gofmt -l** — 列出未格式化文件（格式问题不用人工审查，直接报）
2. **go vet ./...** — 按 go.mod 模块逐个执行静态检查
3. **golangci-lint run** — 可选：未安装则提示并跳过（安装见 https://golangci-lint.run）；项目自带 `.golangci.yml` 时自动使用

**跳过原则**：工具已覆盖的基础问题（格式、未使用变量、vet 警告）不再人工重复报告。

**判别 vet/lint 失败的性质**（先于结论）：
- **环境失败**：模块下载超时（网络不通时用 `GOPROXY=https://goproxy.cn,direct` 前缀重跑）、`//go:embed` 目录缺失（如 `frontend/dist` 未构建）、toolchain 版本缺失——这类要在结论里注明原因，**不算代码问题**。
- **代码失败**：vet 报的 `undefined`、`unreachable`、copylocks 等——进入审查结果。
- gofmt 报"未格式化"若仅为 CRLF 行尾（Windows 常见），脚本已自动识别为行尾提示而非格式错误；根治办法是 `.gitattributes` 加 `*.go text eol=lf`。

### 步骤 2: 规范深度审查（人工 + 知识库）
按审查类别加载对应章节文件后再下结论（不确定某类问题时先读对应章节）：

| 审查类别 | 重点检查 | 加载 |
|---|---|---|
| 命名规范 | 包名/util 禁用、接收器一致、Get 前缀、冗余消除 | ch02, cheatsheet |
| 错误处理 | 哨兵值 vs 字符串匹配、%w 位置、错误信息质量、guard clause | ch06 |
| panic/Fatal 边界 | 业务错误误用 panic、log.Fatal defer 陷阱、Must 函数滥用 | ch13, ch14 |
| 并发与 goroutine | 退出时机、ctx 传递、循环变量捕获（go<1.22）、map 并发写 | ch15, ch09, ch11 |
| 接口与泛型 | 接口在使用侧、编译期验证、泛型克制、any | ch07 |
| 函数设计 | 参数个数、Options 模式、ctx 第一参数、嵌套≤4、同步优先 | ch09, ch16 |
| 类型与拷贝 | 值/指针接收器、Mutex 拷贝、别名问题 | ch16, ch12 |
| 字符串与随机数 | 拼接选型、rune/byte 混淆、Intn vs 取模、crypto/rand | ch18, ch19, ch20 |
| 注释与文档 | 只写不明显内容、契约文档化 | ch03, ch04 |
| 测试质量 | 失败消息格式、断言库禁用、t.Fatal 语义、表驱动结构 | ch24, ch25 |
| 常用库用法 | ctx 不进 struct、json→结构体、Body.Close、flag 命名 | ch22, ch23 |

**快速定位**：嫌疑代码的"看见就该改"清单见 [cheatsheet.md](cheatsheet.md) 的「坏味道速查」；给出修复方案时引用 [patterns.md](patterns.md) 对应模式。

### 步骤 3: 输出结论
问题直接写在对话里，不生成文档文件。严重程度：

| 级别 | 说明 | 示例 |
|------|------|-----|
| 🛑严重 | 功能缺陷、安全漏洞、数据竞争 | SQL注入、goroutine 泄漏、Mutex 拷贝 |
| ⚠️重要 | 规范违反、性能问题 | 错误被吞、%w 位置错误、无容量提示的 map 循环构建 |
| 💡建议 | 风格、最佳实践 | 命名冗余、零值字段未省略 |

### 步骤 4: Todo 清单
```markdown
### 🛑 Critical (必须修复)
- [ ] [并发] goroutine 无退出机制 (worker.go:42) → 参考 ch15

### ⚠️ Major (建议修复)
- [ ] [错误处理] %w 不在错误串末尾 (store.go:87) → 参考 ch06

### 💡 Minor (可选优化)
- [ ] [命名] GetCounts → Counts (counter.go:15) → 参考 ch02
```

---

## 核心规则速览（审查裁决依据）

四原则按序裁决：**逻辑清晰 > 简单 > 简洁 > 可维护性**；一致性冲突以规范为准。

- **命名**：包名小写单数、禁 `util`/`common`；接收器 1-2 字母全类型一致；取值器无 `Get`；变量长度 ∝ 作用域；冗余消除链：包名→接收器→参数→返回值→类型。
- **错误处理**：区分错误用哨兵值 + `errors.Is`，永不 regexp 字符串；`%w` 仅在文档化+测试暴露底层错误时用且放末尾；错误信息 = 原因 + 出错值 + 约束 + 修复法，首字母小写无标点结尾；guard clause 立即返回，嵌套 ≤4 层。
- **panic 边界**：业务/瞬态错误一律 error；终止用 `log.Fatal`（defer 不执行）；panic 只留不可恢复不变量、包内 recover、`log.Fatal` 后 unreachable。
- **并发**：默认同步函数；每个 `go` 语句能回答何时退出（channel 阻塞不被 GC）；goroutine 一律 `process(ctx, item)`；go<1.22 闭包捕获循环变量需复制。
- **接口**：定义在使用侧；不传接口指针；`var _ I = (*T)(nil)` 编译期验证；泛型是最后手段。
- **函数**：参数 >3-4 个 → Options struct / functional options；ctx 永远第一参数、不进 struct、不自定义类型；默认同步。
- **接收器/拷贝**：修改接收者、含 Mutex、含可变指针字段 → 指针；map/chan/func、小值类型 → 值；`*T` 有方法不拷贝 `T`；pb 消息总是指针。
- **测试**：失败消息 `Func(%v) = %v, want %v`；禁断言库，比较用 cmp；`t.Error` 默认、goroutine 里只 Error；表驱动行内无逻辑分支；辅助函数 `t.Helper()` + setup 失败即 Fatal。

完整决策表、阈值（注释≤120列、`+` 栈分配≤32字节等）与坏味道速查见 [cheatsheet.md](cheatsheet.md)。

---

## 章节索引（知识库）

| # | 标题 | # | 标题 |
|---|------|---|------|
| [ch01](chapters/ch01-principles.md) 基本原则 | [ch14](chapters/ch14-must-functions.md) Must 函数 |
| [ch02](chapters/ch02-naming.md) 命名 | [ch15](chapters/ch15-goroutine-lifecycle.md) Goroutine 生命周期 |
| [ch03](chapters/ch03-comments.md) 注释 | [ch16](chapters/ch16-receiver-type.md) 接收器类型 |
| [ch04](chapters/ch04-doc-comments.md) 文档 | [ch17](chapters/ch17-switch-break.md) switch 和 break |
| [ch05](chapters/ch05-imports.md) 导入 | [ch18](chapters/ch18-string-concatenation.md) 字符串拼接 |
| [ch06](chapters/ch06-error-handling.md) 错误处理 | [ch19](chapters/ch19-rune-byte.md) rune 和 byte |
| [ch07](chapters/ch07-builtin-types.md) 内置类型 | [ch20](chapters/ch20-random-numbers.md) 随机数 |
| [ch08](chapters/ch08-composite-literals.md) 字面值格式化 | [ch21](chapters/ch21-type-alias.md) 类型别名 |
| [ch09](chapters/ch09-functions.md) 函数 | [ch22](chapters/ch22-common-libraries.md) 常用库 |
| [ch10](chapters/ch10-indentation.md) 缩进风格 | [ch23](chapters/ch23-complex-cli.md) 复杂 CLI |
| [ch11](chapters/ch11-conditionals-loops.md) 条件与循环 | [ch24](chapters/ch24-testing.md) 测试 |
| [ch12](chapters/ch12-copying.md) 拷贝 | [ch25](chapters/ch25-test-structs.md) 测试结构体 |
| [ch13](chapters/ch13-avoid-panic.md) 避免 Panic | [ch26](chapters/ch26-closing-words.md) 说在最后 |

## 主题索引

- **错误处理** → ch06；**panic/Fatal/Must** → ch13, ch14, ch06
- **命名** → ch02；**注释/文档** → ch03, ch04；**导入** → ch05
- **切片/接口/泛型** → ch07；**格式化/缩进** → ch08, ch10, ch11
- **函数/Options 模式** → ch09（模式详见 patterns.md）；**goroutine/ctx** → ch15, ch22
- **接收器/拷贝** → ch16, ch12；**switch/循环变量** → ch17, ch11
- **字符串/rune/随机数** → ch18, ch19, ch20；**常用库/CLI** → ch22, ch23
- **测试** → ch24, ch25；**坏味道速查** → cheatsheet.md

## Supporting Files

- [cheatsheet.md](cheatsheet.md) — 审查决策表、阈值、坏味道速查（步骤 2 的主要依据）
- [patterns.md](patterns.md) — 修复建议引用的 14 个工程模式
- [glossary.md](glossary.md) — 术语定义
- [scripts/go_check.py](scripts/go_check.py) — gofmt/vet/golangci-lint 聚合工具
- chapters/ — 指南 26 章详细规则（按需加载）

## Scope & Limits

审查依据为《腾讯 GO 语言编程指南》（Go 1.18+）+ gofmt/go vet/golangci-lint 工具输出；不覆盖指南未收录的社区争议话题。工具未覆盖安全扫描（如需用 `govulncheck`）；golangci-lint 未安装时跳过该步并提示。与 `code-review` skill 的区别：本 skill 专注 Go 规范，多语言审查用 code-review。
