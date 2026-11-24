# 第 15 章: Goroutine 生命周期

## Core Idea
创建 goroutine 时必须明确它**何时/是否退出**——GC 永远不会回收阻塞在 channel 上的 goroutine。

## Frameworks Introduced
- **泄漏机理**: goroutine 阻塞在 channel 收发时即使无人引用 channel 也不会被 GC 终止。向已关闭的 channel 发送会 panic。结果不再需要后修改仍被使用的输入 → 数据竞争；goroutine 任意长运行 → 不可预测的内存占用。
- **生命周期显式化**: 把同步代码限制在函数范围内、拆分为同步函数（见第 9 章）；并发仍不明显时**文档化 goroutine 何时/为何退出**。
- **context 管理模式**: `go process(ctx, item)` ——process 至迟在 ctx 取消时返回。也可用原始信号通道（`chan struct{}`）、同步/条件变量，但对维护者而言退出时机不如 ctx 明显。

## Anti-patterns
- **`go process(item)` 无 ctx**: 不关心 goroutine 何时完成——生产中可能未定义行为、无法优雅停止、难以针对性测试、资源泄漏。

## Code Examples
```go
// Good: ctx 控制 goroutine 退出
func (w *Worker) Run(ctx context.Context) error {
    for item := range w.q {
        go process(ctx, item) // 至迟在 ctx 取消时返回
    }
    return nil
}
```
- **What it demonstrates**: goroutine 的退出条件编码进 ctx。

## Key Takeaways
1. 每个 `go` 语句都要能回答"它什么时候退出"。
2. channel 阻塞不被 GC；向已关闭 channel 发送 panic。
3. 默认同步函数；goroutine + ctx 是标准并发姿势。

## Connects To
- **Ch 9 函数**: 同步函数优先原则。
- **Ch 22 常用库**: Context 用法。
