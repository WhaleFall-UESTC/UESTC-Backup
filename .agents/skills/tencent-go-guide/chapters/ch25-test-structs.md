# 第 25 章: 测试结构体

## Core Idea
子测试独立、命名规范；表驱动测试不含执行逻辑分支；辅助函数 t.Helper + Fatalf setup 失败；测试包用 abc_test.go 或 `_test` 包；静态检查用 go vet + Staticcheck。

## Frameworks Introduced
- **子测试（t.Run）**: 灵活的 setup/清理、并行控制、测试过滤；非强制。子测试初始状态不依赖其他测试。命名用**命名函数规范**（空格会被换下划线、非打印字符被转义）；**避免斜杠**（测试过滤器的特殊含义：`t.Run("America/New York")` 使 `--test_filter="Time/New_York"` 匹配不到）。长描述放独立字段（`desc`），失败时 `t.Log`/消息一起打印。
- **表驱动测试**: 多用例共享测试逻辑时使用。两特征：特定输入→期望输出；输出在特定范围。
  - **拆分逻辑不同的用例**: 判断逻辑不同 → 多个测试函数（如成功用例表 + 错误用例表各一个函数）；输入输出类型相同但检查逻辑不同 → 单函数内多个子测试系列。同一函数的测试保持一致的输入检查逻辑。
  - **允许数据冗余**: 行值复杂时允许用例间字段冗余换可读性（两组表各带 codex 字段，好过一张表 + switch 初始化）。
  - **数据驱动陷阱**: 不要在表行里放"环境选择"枚举再在循环里 switch 初始化（fake/prod 分支）——按环境拆成两个测试函数。
  - **不用序列号报错**: `failed on case #%d` → 必须打印用例名。
  - **字段标签**: 用例超过 20-30 行、有同类型相邻字段、想省零值字段时，字面量带字段名。
- **测试辅助函数**: 执行 setup/清理，**不是通用断言工具**；其中故障应是基础环境故障。规范：
  - `t.Helper()` 使失败归属调用处行号（辅助函数间嵌套、使用量大时尤其重要）。
  - 执行位置：ctx 参数之后、测试逻辑之前。
  - setup 失败 → 辅助函数内直接 `t.Fatalf("Setup failed: ...")`，比返回 error 给调用方清晰（`mustAddGameAssets` 模式）。
  - Fatal 消息带上下文（在哪、为什么）。
  - **不触发失败的辅助函数不要 t.Helper**，并从签名中删掉 `*testing.T`。
  - Go 1.14+ 可用 `t.Cleanup` 注册清理。
- **测试包组织**:
  - 同包测试：`abc.go` ↔ `abc_test.go`，可访问未导出标识符，覆盖率更好。
  - `_test` 包（`package fireworks_test`）：集成测试无明显归属库时、同包测试造成循环依赖时。是包名"无下划线"规则的唯一例外。
- **testing 包**: 不用断言库与第三方测试框架——为用例维护带来额外复杂性。
- **静态检查**: `go vet` + `Staticcheck`（更多见 awesome-go: Code Analysis）。

## Anti-patterns
- **表行驱动环境选择**: `codex testCodex`（fake/prod 枚举）+ 循环内 switch 初始化 → 拆成 `TestDecode` / `TestDecodeWithFake`。
- **`failed on case #%d`**: 序列号无信息量。
- **子测试名含斜杠或超长描述**。
- **忘 t.Helper**: 失败行号指向辅助函数内部而非调用处。

## Code Examples
```go
// Good: 标准辅助函数
func mustAddGameAssets(t *testing.T, dir string) {
    t.Helper()
    if err := os.WriteFile(path.Join(dir, "pak0.pak"), pak0, 0644); err != nil {
        t.Fatalf("Setup failed: could not write pak0 asset: %v", err)
    }
}

// Good: 表驱动 + 子测试
for _, test := range tests {
    t.Run(test.name, func(t *testing.T) {
        got, err := Decode(test.input, codex)
        if got, want := got, test.output; got != want {
            t.Errorf("Decode(%q) = %v, want %v", test.input, got, want)
        }
        if got, want := err, test.err; !cmp.Equal(got, want) {
            t.Errorf("Decode(%q) err %q, want %q", test.input, got, want)
        }
    })
}
```
- **What it demonstrates**: must* setup 模式 + t.Run 子测试 + cmp 错误比较。

## Key Takeaways
1. 子测试名如函数名、无斜杠；描述放 desc 字段。
2. 表驱动一行一用例、行内无逻辑分支；不同环境/逻辑拆成不同测试函数。
3. 辅助函数 = setup/清理 + t.Helper + 失败即 Fatalf；通用断言辅助不存在（返回 error）。
4. 同包测未导出；集成/循环依赖用 `_test` 包。
5. go vet + Staticcheck 常开。

## Connects To
- **Ch 24 测试**: 失败消息格式与 Fatal 语义。
- **Ch 8 字面值格式化**: 表行字段标签规则的同源。
