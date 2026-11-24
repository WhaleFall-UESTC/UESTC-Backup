# 第 6 章: 错误处理

## Core Idea
错误是值，创建与消费都要有明确目的：调用者需要区分错误条件时给错误**添加结构**（哨兵值/结构体），处理错误时**添加本层独有信息**——绝不字符串匹配，也绝不盲目传播。

## Frameworks Introduced
- **错误结构（哨兵值）**: 调用者需查询错误时用全局哨兵值（`ErrDuplicate = errors.New("duplicate")`），调用方 `==` 比较或包装后用 `errors.Is(err, ErrDuplicate)`。**绝不 regexp 匹配错误字符串**。需要附带数据时用结构化错误（如 `os.PathError{Op, Path, Err}`）。
- **添加信息原则**: 错误传播链中每层考虑添加"本层独有的意义"，但避免重复/无关细节。`os.Open("settings.txt")` 的错误已含路径——外层再写 `could not open settings.txt: %w` 就是冗余。判断方法：开发期触发错误条件，看观察者最终得到什么。
- **%w 包装的代价**: `fmt.Errorf("%w")` 允许调用者 `errors.Is/Unwrap` 访问原始错误，但**以不明显的方式扩大了包的 API 表面**——改实现细节可能造成破坏。**不要用 %w，除非你文档化并有测试验证所暴露的底层错误**；不期望调用方 unwrap 就不要用。
- **%w 位置**: 放在错误串末尾（`[...]: %w`）。每层包装在链前端加新条目，`Unwrap` 从最新到最旧遍历。`%w` 在中间或开头会导致错误文本顺序与链顺序不一致，破坏可读性。
- **错误串风格**: error 通常作为函数**最后一个出参**，nil 表示成功。错误字符串**首字母不大写、结尾不加标点**——它是短语不是完整句子。

## Key Concepts
- **有用错误信息回答两问**: ① 出了什么问题？② 用户如何解决？坏信息的特征：模糊、不准确、不精确、无法行动；好信息：可行动、可自助、减轻支持负担。
- **四条通用准则**: ① 不要默默失败（不报告失败不可原谅；例外：协调一组操作时只有第一个错误有用——用 errgroup）；② 不吞掉根本原因（"服务器错误"过于笼统，要区分服务故障/网络中断/状态不匹配/权限问题）；③ 记录错误码（内部/外部错误分别指定，文档化所有错误码）；④ 尽早暴露错误（稍后暴露显著增加调试成本）。
- **解释问题三技法**: ① 具体说明原因（`bad directory` → 说明目录存在但不可写、如何修复）；② 识别无效输入（把出错值打进去：`postal code (%s) contained (%d) digits`；超长多行值用省略号逐步披露或截断保留关键内容）；③ 写明要求与约束（附件 `(%dMB) exceeds the allowed limit (10MB)`，不要假设用户知道限制）。
- **给出解决方案**: 解释原因后解释如何修复；提供正确示例（`for example: %s@example.com`）；给出 before/after 对照。
- **内置错误处理**: 不用 C 风格的 -1/null/空串表示错误；用多返回值避免零值歧义——`Lookup(key string) (value string, ok bool)`。

## Anti-patterns
- **字符串匹配错误**: `regexp.MatchString("duplicate", err.Error())` ——结构化信息退化。
- **冗余包装**: `fmt.Errorf("could not open settings.txt: %w", err)` 当 `err` 已含路径。
- **else 分支吞错误**: `if err != nil {...} else { 正常代码 }` ——错误应立即返回，正常路径不缩进（缩短错误路径 / guard clause）。
- **panic 当错误处理**: 库应返回错误而非中止程序；**也不要 recover panic 防崩溃**——可能传播损坏状态、远离 panic 处对状态了解更少、可能持有锁。用监控发现意外 panic 并修 bug。注：标准库 net/http 从 handler recover panic 是公认的历史错误，自己的 server 别模仿。
- **init 里调日志**: 解析 flags 之前不要调用日志函数；必须在 init 中终止时用 panic 代替日志调用。

## Code Examples
```go
// Good: 哨兵值 + errors.Is
switch err := process(an); {
case errors.Is(err, ErrDuplicate):
    return fmt.Errorf("feed %q: %v", an, err)
case errors.Is(err, ErrMarsupial):
    // ...
}

// Good: %w 放末尾，错误文本顺序 = 链顺序
err1 := fmt.Errorf("err1")
err2 := fmt.Errorf("err2: %w", err1)
err3 := fmt.Errorf("err3: %w", err2)
fmt.Println(err3) // err3: err2: err1

// Good: 缩短错误路径
if err != nil {
    return // or continue, etc.
}
// normal code
```
- **What it demonstrates**: 结构化查询、链顺序、guard clause 三大日常模式。

## Worked Example
初始化错误路径：错误的 flags/配置向上传播到 main，main 调 `log.Fatal` 并附"如何修复"的信息。不可达分支（`log.Fatalf` 之后）补 `panic("unreachable")` 帮助编译器识别不可达。真正的内部不变量破坏用 `log.Fatal` 而非 panic（延迟函数可能死锁或进一步破坏状态）。

## Key Takeaways
1. 区分错误靠哨兵值 + `errors.Is`，从不靠字符串；需要数据就定义结构化错误。
2. `errors.Is` 比较错误值；`%v` 包装提供上下文；`%w` 只在你想暴露底层错误（且文档化+测试）时用，且放在末尾。
3. 好错误信息 = 具体原因 + 出错值 + 要求/约束 + 修复方法/示例；首字母小写、无结尾标点。
4. 错误立即返回（guard clause）；初始化错误传播到 main 由 `log.Fatal` 报告。
5. panic 只用于：API 误用（不可恢复的不变量）、包内部带匹配 recover 的实现细节（不跨包边界）、`log.Fatal` 后的 unreachable。

## Connects To
- **Ch 13 避免 Panic / Ch 14 Must 函数**: panic 使用的边界。
- **Ch 24 测试**: 关心错误条件的测试同样适用结构化错误。
