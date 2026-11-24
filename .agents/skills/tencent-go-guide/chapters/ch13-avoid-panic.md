# 第 13 章: 避免 Panic

## Core Idea
panic 不用于正常错误处理；该终止程序的场景用 `log.Fatal`（注意它跳过所有 defer）。

## Frameworks Introduced
- **错误处理用 error + 多返回值**: 见第 6 章。
- **log.Fatal 适用场景**: main 包和初始化代码中应终止程序的错误（无效配置）——此时堆栈跟踪对读者没有帮助。**注意**: `log.Fatal` 调用 `os.Exit`，**defer 不会运行**。
- **"不可能"条件**: 代码审查/测试本应捕获的编程/逻辑错误——函数可返回错误让调用者处理，或直接 `log.Fatal`，两者都明确表达严重性。

## Key Takeaways
1. 业务错误路径一律 error；panic 只留给第 6 章定义的三种场景。
2. 终止程序用 `log.Fatal` 而非 panic，但要知道 defer 不会执行。
3. 无效配置在 main/初始化阶段 Fatal，不留到运行时。

## Connects To
- **Ch 6 错误处理**: 何时使用 panic 的完整论述。
- **Ch 14 Must 函数**: 初始化期终止的惯用封装。
