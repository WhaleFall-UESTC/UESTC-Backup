# 第 9 章: 函数

## Core Idea
签名一行放下、参数靠选项结构体或可变参数选项收敛、优先同步函数、嵌套 ≤4 层用卫语句压平。

## Frameworks Introduced
- **函数传值**: 不要为省字节传指针——函数全程只用 `*x` 解引用时直接传 `x`（典型：`*string`、`*io.Reader`，都是固定大小值）。**例外**：大型结构体、可能变大的小结构体；**pb 消息总是指针传递**。
- **签名一行化**: 函数/方法签名保持一行。手段：拆解局部变量（`local := helper(...)` 再传入）、封装结构体、可变参数选项。**拆解导致理解变难就不要拆**——坐标渲染这类场景多参数换行反而更清晰，始终允许。
- **不给参数加行内注释**: `42, // Port` → 用 `server.Options{Port: 42}`。
- **封装结构体（Options struct）**: 参数打包成结构体作为最后一个参数。优点：字面量自带字段名、可省略默认字段、字段文档更清晰、加参数不破坏调用方。适用：所有调用者都需指定某些选项；不同调用者参数差异大；选项跨多个函数共享。**context 绝不封装进结构体**——永远是第一个显式参数。
- **可变参数选项（functional options）**: `type XOption func(*xOptions)` + 导出构造器闭包。优点：不配置零开销、一个选项可接多参、返回命名类型可读、支持他包自定义。适用：大多数调用者不需要任何 option；option 很少用/很多/带参数/可能失败（返回错误）/需要大量文档/允许第三方扩展。
- **同步函数优先**: 同步函数把 goroutine 限制在调用范围内，便于生命周期管理和测试（传输入查输出，无需轮询）。调用者想要并发可自己 go 出去；**上线后再删除不必要的并发几乎不可能**。
- **嵌套深度 ≤4 层**: 错误处理用卫语句（`if err != nil { return err }` 顺序排列）代替 `if err == nil { if ... }` 金字塔。

## Anti-patterns
- **`*string`/`*io.Reader` 参数**: 固定大小值传指针纯属噪音。
- **超长参数列表**: 9 个参数的 `EnableReplication(ctx, config, regions..., bool, bool, time.Duration, int, watcher)` → Options struct 或 functional options。
- **参数行内注释**: 用 Options 字段名或文档代替。
- **异步函数当默认**: 强迫每个调用者处理 goroutine 生命周期。
- **err==nil 金字塔**: 四层嵌套的 else return。

## Code Examples
```go
// Good: 卫语句压平嵌套
if err := bar(); err != nil {
    return err
}
if err := baz(); err != nil {
    return err
}
// ...

// Good: functional options 模式
type ReplicationOption func(*replicationOptions)
func ReadonlyCells(cells ...string) ReplicationOption {
    return func(opts *replicationOptions) {
        opts.readonlyCells = append(opts.readonlyCells, cells...)
    }
}
func EnableReplication(ctx context.Context, cfg *Config, cells []string, opts ...ReplicationOption)
```
- **What it demonstrates**: context 保持在第一参数位；选项闭包更新私有 options 结构。

## Key Takeaways
1. 固定大小值直接传；pb 消息、大结构体传指针。
2. 参数 >3~4 个就考虑 Options struct（调用者常需配置）或 functional options（配置可选/可扩展/可失败）。
3. ctx 是第一个参数，永不进 Options。
4. 默认写同步函数；并发由调用方决定。
5. 嵌套 ≤4 层；错误检查全部用卫语句。

## Connects To
- **Ch 6 错误处理**: 卫语句即"缩短错误路径"。
- **Ch 15 Goroutine 生命周期**: 异步函数的生命周期管理代价。
