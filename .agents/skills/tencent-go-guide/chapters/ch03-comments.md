# 第 3 章: 注释

## Core Idea
注释为读者服务：行注释为主，块注释主要用于包注释；格式与代码对齐（≤120 列），遵循标准英语语法；签名含义不清时靠返回值类型与注释补信号。

## Frameworks Introduced
- **注释类型选择**: `//` 行注释最常用；`/* */` 块注释主要用于包注释，也可用于复杂语句说明。**不要用块注释禁用代码**——不需要的代码直接删除，需要时从版本管理恢复。
- **格式规范**: 注释列宽与代码一致（120 列以内）；可导出与否用相同注释风格；标准英语语法（首字母大写、规范标点）。
- **命名返回结果**: 裸返回（naked return）仅在小函数中可接受；函数变中等大小就明确返回值。**不要仅因函数长就命名结果参数——签名是否清晰才是最终考量**。返回语义不明的 func 时用具名类型（`cancel func()` 前先考虑 `CancelFunc`）。

## Code Examples
```go
// Good: 注释说明返回值契约而非复述参数
// WithTimeout returns a context that will be canceled no later than d
// duration from now.
//
// The caller must arrange for the returned cancel function to be called
// when the context is no longer needed to prevent a resource leak.
func WithTimeout(parent Context, d time.Duration) (ctx Context, cancel func())
```
- **What it demonstrates**: 注释价值在于说明隐含契约（必须调用 cancel 防泄漏），而不是复述 `parent` 是父 context。

## Key Takeaways
1. 行注释为主，块注释留给包注释；禁用代码 = 删除 + VCS。
2. 注释 ≤120 列，标准英语语法，导出与否风格一致。
3. 裸返回只在小函数中用；签名清晰优先于命名返回参数。

## Connects To
- **Ch 4 文档**: 导出符号的文档注释（Godoc）约定。
- **Ch 9 函数**: 函数签名设计本身。
