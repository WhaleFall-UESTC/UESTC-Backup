# 第 24 章: 测试

## Core Idea
失败消息要让人不读源码就能诊断（函数名 + 输入 + got/want）；禁用断言库；比较用 cmp 包；测试连续执行（t.Error 优先）；错误语义测存在性不测字符串。

## Frameworks Introduced
- **失败消息四要素**: 什么导致失败、哪些输入、实际结果、预期。标准格式 **`YourFunc(%v) = %v, want %v`** ——got 在 want 之前，用 got/want 词汇。失败消息包含函数名（即使测试名已 obvious）；输入短就打印输入，输入大/不透明就用描述命名用例并打印描述。
- **禁用断言库**: 不创建/不使用把验证和失败消息生成绑定的 assert 库——它们倾向提前终止（Fatalf/panic）、丢失上下文、风格碎片化。用 Go 自带 testing + 标准库 `cmp`/`fmt`。
  - **特定类型的比较辅助**返回 `error` 而非接收 `*testing.T`——`if err := CompareUsers(got, want); err != nil { t.Errorf(...) }`。
- **全结构比较**: 返回结构体/切片/map 时构造期望值整体深度比较，不逐字段编码比较。多返回值则逐个比较打印。
- **比较稳定的结果**: 不比较依赖第三方输出的字节——`json.Marshal` 的具体字节过去变过。改测语义：unmarshal 回结构体再 DeepEqual。
- **t.Error vs t.Fatal**: 测试应连续执行，一次运行打出所有失败（修复者不必修一个跑一次）。
  - 默认 `t.Error`；只有**后续步骤无法继续**时 `t.Fatal`。
  - 表驱动无子测试：`t.Error` + `continue`；有子测试（t.Run 内）：`t.Fatal`（只终止当前子测试）。表驱动优先考虑子测试 + t.Fatal。
  - **绝不从测试 goroutine 之外调用 t.Fatal/t.FailNow**——goroutine 里用 `t.Errorf` + return。`t.Parallel` 不影响此安全性。辅助函数在测试 goroutine 上用 Fatal 完全正确。
- **相等与差异**: `==` 仅限标量；复杂数据用 `cmp.Equal` / `cmp.Diff`（差异输出可读性强）。
- **打印细节**: 字符串用 `%q`；小结构用 `%+v`；大值打印差异（cmp.Diff）不打印全量。初始化失败只需 `t.Fatalf("Setup: ... %s", err)` 不需要常规详略。
- **错误语义**: 单测**不比较错误字符串**（易被修改）；通常只测 `err != nil` 存在性；需语义匹配时用 `cmpopts.EquateErrors`。允许字符串检查被测包错误消息是否含参数名等属性。
- **真实传输协议**: 集成测试用真实 client + server 测试替身（如 httptest），不手写模仿 client——模仿正确性复杂，真实 client+测试 server 让测试尽量多跑真实代码。能用被测服务作者提供的测试库就用。

## Code Examples
```go
// Good: 标准失败消息格式
if got, want := Add(2, 3), 5; got != want {
    t.Errorf("Add(%d, %d) = %d, want %d", 2, 3, got, want)
}

// Good: goroutine 内错误报告
go func() {
    defer wg.Done()
    if err := engine.Vroom(); err != nil {
        t.Errorf("No vroom left on engine: %v", err) // 不能 t.Fatal
        return
    }
}()
```
- **What it demonstrates**: YourFunc(in) = got, want 格式；goroutine 中的 Fatal 禁令。

## Key Takeaways
1. 失败消息 = 函数名(输入) = got, want；got 在前。
2. 不用断言库；比较用 cmp 包；辅助比较函数返回 error 不收 *testing.T。
3. t.Error 默认、t.Fatal 仅限"无法继续"；goroutine 里只 Error。
4. 测错误存在性/语义（cmpopts），不测错误字符串。
5. 集成测试走真实传输协议。

## Connects To
- **Ch 6 错误处理**: 哨兵值/结构化错误使错误语义测试成为可能。
- **Ch 25 测试结构体**: 子测试与表驱动测试的组织。
