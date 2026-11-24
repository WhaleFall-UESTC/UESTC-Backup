# 第 11 章: 条件与循环

## Core Idea
if/for/switch 声明一律一行放下；放不下就提取布尔变量/局部变量缩短条件，绝不人为换行制造缩进混乱；循环迭代变量在 go1.22 前有共享陷阱。

## Frameworks Introduced
- **if 不换行**: 多行 if 子句导致缩进混乱。缩短手段：① 不需要短路时提取布尔操作数（`inTransaction := ...; keysMatch := ...; if inTransaction && keysMatch`）；② 提取局部变量代替重复调用（`uid := user.GetUniqueUserID()`）。包含闭包/多行结构字面量的 if 确保大括号成对匹配。
- **for 不换行**: 没有优雅重构就保留长语句；通常可把后半条件移入循环体 `if collection.HasPendingWriters() { break }`。
- **switch/case 一行**: `case A, B, C:` 列在同一行。过长时缩进所有 case 并用空行分隔。
- **Yoda 条件禁止**: 常量比较时变量放左侧——`if result == "foo"`，不要 `if "foo" == result`。
- **循环迭代变量陷阱（go1.22 前）**: 循环变量是**单个变量**逐次赋值，Go 官方视其为语言错误，**go1.22 已修复**（每次迭代新变量）。
  - 取地址进切片：`&i` 全部指向同一地址 → 输出 3 3 3。修复：`i := i` 复制。
  - 闭包/goroutine 引用：所有 goroutine 竞态同一 `v` → 输出全 5。修复：`v := v` 或作为参数传入 `go func(v int)`。
  - go1.22+ 两种代码都直接正确。

## Anti-patterns
- **if 条件换行且与块体对齐**。
- **Yoda 风格**: 常量在左的比较。
- **go1.22 前闭包直接捕获循环变量**。

## Code Examples
```go
// Good: 提取布尔变量避免 if 换行
inTransaction := db.CurrentStatusIs(db.InTransaction)
keysMatch := db.ValuesEqual(db.TransactionKey(), row.Key())
if inTransaction && keysMatch {
    return db.Error(...)
}
```
- **What it demonstrates**: 条件缩短优先于换行。

## Key Takeaways
1. if/for/switch/case 声明一行；缩短条件靠提取变量，不靠换行。
2. 变量在等号左侧（反 Yoda）。
3. go<1.22 闭包/取址捕获循环变量必须复制（`i := i`）；go1.22+ 无此问题，但了解历史代码为何这么写。

## Connects To
- **Ch 10 缩进风格**: 同一根源的缩进混乱问题。
- **Ch 15 Goroutine 生命周期**: goroutine 捕获循环变量的并发面。
