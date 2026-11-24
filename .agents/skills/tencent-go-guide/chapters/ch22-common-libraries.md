# 第 22 章: 常用库

## Core Idea
标准库优先（flag/Context/sync/encoding/json/net/http/embed）；ctx 永远第一个参数且不进结构体、不自定义类型；JSON 解码进结构体不进 map；http Body 必 defer Close。

## Frameworks Introduced
- **flag**: 标志名蛇形命名（`poll_interval`），对应变量驼峰（`pollInterval`）；只在 main 包或同等入口包使用。
- **context.Context**: 在 API 与进程边界间传安全凭据/追踪信息/截止时间/取消信号，整个调用链显式传递。
  - **第一个参数规则**: `func F(ctx context.Context, ...)`。例外：HTTP handler 用 `req.Context()`；流式 RPC 的 ctx 来自流；入口点用 `context.Background()`（main、init、TestXXX/BenchmarkXXX/FuzzXXX）。
  - **不放进结构体**: 需要的类型每个方法加 ctx 参数（唯一例外：签名必须匹配标准库/三方接口，罕见）。
  - **ctx 不可变**: 同一 ctx 可传给共享截止时间/取消信号/链路追踪的多个调用。
  - **不自定义 Context 类型**: 每团队自定义会导致 p.Context→q.Context 转换地狱、自动重构不可能。应用数据放参数/接收器/全局变量，真正属于 ctx 的才放 Context 值。
- **sync.Mutex**: 作为被保护资源结构体的字段；`mu.Lock(); defer mu.Unlock()` 推荐模式（防分支漏 Unlock）；避免持锁调用可能再取锁的函数防死锁。
- **encoding/json**: 优先标准库，避免无人维护的第三方库缺陷/安全问题无法及时修复。**避免 `map[string]interface{}` 解码**——用结构体（`type Promotion struct{ID int; Name string; Discount float64}`）获得类型安全。
- **net/http**: **`defer resp.Body.Close()` 必写**，否则资源泄漏。检查 err 后立即 defer，再读 Body。
- **embed**: Go 1.16+ 把静态资源嵌入二进制（`//go:embed hello.txt; var f embed.FS`，运行时 `fs.ReadFile`/`f.ReadFile`）。注意：大文件显著增加二进制体积、大量文件影响性能（可改文件服务器/CDN）；`//go:embed` 路径必须**相对 Go 文件**，绝对路径跨环境不可用。

## Anti-patterns
- **结构体存 ctx 字段**。
- **自定义 Context 接口类型**。
- **Unlock 不 defer**: 多分支返回时漏 Unlock 死锁。
- **JSON 解码进 map[string]interface{}**: 丢类型安全。
- **忘 close resp.Body**。

## Code Examples
```go
// Good: flag 命名
var pollInterval = flag.Duration("poll_interval", time.Minute, "Interval to use for polling.")

// Good: embed
//go:embed hello.txt
var f embed.FS
```
- **What it demonstrates**: 蛇形 flag 名 + 驼峰变量名；embed 指令。

## Key Takeaways
1. ctx：第一参数、不进 struct、不自定义类型、入口点 Background()。
2. Mutex 紧贴被保护资源 + defer Unlock。
3. JSON → 结构体；http → defer Body.Close()；静态资源 → embed（路径相对）。

## Connects To
- **Ch 15 Goroutine 生命周期**: ctx 取消是 goroutine 退出的标准信号。
- **Ch 1 基本原则**: 标准库优先原则的落地章节。
