---
name: go-deslop
description: "为 Go 而生的 AI 代码去味（deslop）：提交前清理 AI 生成的冗余代码——过度设计、不可能场景的错误处理、明显注释、未请求的顺手优化；核心标准是最小实现（能否删掉而不影响任务）。以《腾讯 GO 语言编程指南》为判据，删除后验证仅移除 slop 且编译/测试通过。Use before committing, when user says deslop/清理/删冗余/瘦身代码, or reviewing AI-written Go code for over-engineering."
---

<!-- argument-hint: [范围：未提交变更(默认) / 文件 / commit 范围] -->

# go-deslop — 为 Go 而生的 AI 代码去味

对 Go 代码 diff 做一次**只删不加**的清理 pass：移除 AI 生成的 slop（过度设计、冗余防御、废话注释、未请求的功能），**验证删的只是 slop、行为不变**，然后汇总。

## 核心审查标准（每个改动块必问）

> **要实现这个任务，目前的方案是否是最小的？是否有可以删除的地方？**

逐文件、逐函数、逐字段过一遍，候选删除项包括但不限于：用不上的参数/字段/返回值、只有一个实现者的抽象层、永远走不到的分支、没人调用的导出函数、"为将来预留"的配置项。**删掉后任务仍然成立的东西，就是 slop。** 判据出处：《腾讯 GO 语言编程指南》ch01（简单/简洁：功能简单但逻辑复杂 = 不必要复杂性；重复代码、多余代码是简洁性最大敌人）与项目 AGENTS.md Karpathy 准则（每行改动都应能追溯到用户请求）。

## Slop 模式清单（Go 版）

**必须引用判据再动手**：规范细节见同级 skill `tencent-go-guide`（`chapters/` 与 `cheatsheet.md`），下面只列模式与出处。

1. **非最小方案**（核心标准，见上）—— ch01
2. **不可能场景的错误处理** —— 为不会失败的调用写 err 检查、给不可能为 nil 的值加 nil 防御、给内部函数包一层只做转发的 error 包装。注意例外：`log.Fatal` 之后的 `panic("unreachable")` 是指南认可的惯用法，**保留**（ch06）。
3. **过度抽象** —— 只有一个实现者的接口（ch07：接口定义在使用侧、最小公开）；2 个参数就上 Options struct；所有调用者都设置的 functional option（ch09 的选用条件反着用就是 slop）；单次使用的泛型、能用具体类型却用 `any`（ch07）。
4. **未请求的功能与顺手优化** —— 任务之外的重构、预留的灵活性/配置项、没人要求的"性能优化"（如无必要的手动容量预分配，ch07 明说普通代码不需要）。
5. **明显注释与废话文档** —— 复述代码的注释（`// 调用 foo`）、给未改动代码新加的 docstring、注释掉而不删的死代码（ch03：禁用代码 = 删除 + VCS）。
6. **向后兼容/预留 hack** —— 没有第二个调用方的"通用"工具函数、为将来留的 TODO 脚手架、重命名的 `_var` 规避 unused 检查。
7. **深嵌套与防御层叠** —— 金字塔嵌套可被卫语句压平（ch09/ch06）；连续多层 nil/err 检查同一值。

**不是 slop，别删**：指南认可的惯用法（guard clause、`defer mu.Unlock()`、表驱动测试的显式零值字段）、修复真实 bug 的防御、用户明确要求的功能。

## 工作流

**0. 范围** → **1. 找 slop** → **2. 只删不加** → **3. 验证** → **4. 汇总**

### 0. 确定范围
- 默认：**未提交变更**（`git diff HEAD --stat` + `git diff HEAD`，含已暂存）——最贴合"提交前清理"
- 指定文件：直接审查
- commit 范围：`git diff <a>..<b>`（本仓库默认分支为 master，无 main；不要执行 `git fetch origin main`，本机到 GitHub 网络不稳，需要远端基准时用本地 `master`）
- 多模块注意：client/ 与 server/ 是两个独立 go.mod

### 1. 找 slop
读 diff，按上面清单逐模式过；拿不准的块先读 `../tencent-go-guide/chapters/ch<N>.md` 或 `cheatsheet.md` 再定性。输出候选清单（文件:行 + 模式 + 判据章节）。

### 2. 只删不加
- 只做**删除与最小收缩**：不重排、不改名、不换风格、不"顺手修正"无关问题
- 删注释时连同失效的空行/孤立括号一起清干净；删函数时同步删掉仅它使用的 import/变量/常量
- 有歧义的（可能是用户要的）→ 不删，记入"待确认"清单

### 3. 验证（每轮删除后）
```bash
# 按模块执行（在仓库根目录或模块目录下）
go build ./... && go vet ./...
go test ./...    # 有测试时必跑
# 网络不通时加前缀: GOPROXY=https://goproxy.cn,direct
```
- **重新 diff 对比**：`git diff` 确认改动只包含 slop 移除，没有行为变更混入
- 也可以用 `../tencent-go-guide/scripts/go_check.py --skip-golangci` 一步跑 gofmt+vet
- 编译或测试失败 → 说明删多了，回滚那一处重新定性

### 4. 汇总
```markdown
## deslop 结果 — <范围> — <日期>
已删除:
- <file:line> [<模式>] <删了什么>（判据 ch<N>）
保留待确认:
- <file:line> <为什么没删，问用户>
验证: go build/vet/test <结果>；diff 仅含 slop 移除
```

## 与其他 skill 的分工

- 审查报告（找 bug、安全、规范全量检查）→ `tencent-go-guide` / `code-review`
- 中文文档去 AI 味 → `de-ai-ify`
- 本 skill：**提交前只删不加的瘦身 pass**
