# 第 4 章: 文档

## Core Idea
文档只写**不明显、易出错、有实质帮助**的内容；Go 社区的隐含契约（ctx 可取消、只读并发安全、修改需同步）默认不写，偏离默认时才必须写。

## Frameworks Introduced
- **参数和配置文档原则**: 不是每个参数都要列举。函数参数、结构体字段、Options API 只记录**易出错或不明显**的字段，说明为什么需要关注。受众包括维护者、新人、外部用户和六个月后的你自己。
- **上下文（context）契约**: `ctx` 参数意味着函数可被取消；可返回错误时通常返回 `ctx.Err()`——这是隐含契约，**不需重述**。以下偏离必须写明：
  - 取消时返回 `ctx.Err()` 以外的错误
  - 有其他中断/生命周期机制（如还有 `Stop` 方法做同步 graceful shutdown，而 ctx 取消是异步内部处理，run 可能提前返回）
  - 对 ctx 生命周期有特殊要求（如 "context should not have a deadline"、必须带 `security.NewContext` 的 value）
  - **警告**: 避免设计出要求调用方满足特殊 ctx 条件的 API；文档只是无法避免时的补救，不是对该模式的认可。
- **并发契约三默认**: Go 用户默认——概念上只读操作并发安全（不用写）；修改操作默认不安全、需用户自行同步（不用写）。**必须写文档的三种情况**：
  1. 分不清是只读还是修改（如 LRU 的 `Lookup` 命中会内部改变缓存顺序）
  2. 同步由 API 提供（如 rpc 客户端支持多 goroutine 并发；类型整体提供同步时只在类型定义处记录）
  3. API 消费用户实现的接口且对该接口有特殊并发要求（如 `Watcher` 接口的方法必须并发安全——这是契约的一部分）
- **清理契约**: 记录所有明确的清理要求及责任方——`Call Stop to release... when done`；清理方式不明确时给出完整用法示例（`http.Get` 示例展示 `defer resp.Body.Close()`）。

## Anti-patterns
- **复述式注释**: `// format is the format, and data is the interpolation data` ——零信息量。
- **重述隐含契约**: `// The method will process work until the context is cancelled and accordingly returns an error` ——ctx 契约默认如此，删掉。
- **漏写非默认行为**: ctx 取消时返回 nil error 却不写，调用方会按 `ctx.Err()` 处理而出错。

## Key Takeaways
1. 只文档化"不明显"的内容；Go 并发/ctx 默认契约不重述，偏离必写。
2. 清理责任（Stop/Close/defer）必须写，最好附可运行示例。
3. 可运行示例（`Example` 函数）会出现在 Godoc 和代码搜索中，是解释用法的最佳方式。

## Connects To
- **Ch 24 测试**: `Example` 测试函数的可运行示例写法。
- **Ch 22 常用库**: Context 的正确使用姿势。
