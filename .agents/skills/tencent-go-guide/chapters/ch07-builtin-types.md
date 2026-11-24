# 第 7 章: 内置类型

## Core Idea
切片/映射用 nil 初始化、按 len 判断、给 make 容量提示；接口定义在使用侧、传值不传指针、编译期验证实现；泛型克制使用，`any` 逐步替换 `interface{}`。

## Frameworks Introduced
- **nil 切片**: 空切片用 `var t []string`（nil）初始化，减少使用者出错风险。**接口设计不得区分 nil 切片与空切片**——判断用 `len(s) == 0` 而非 `s == nil`，否则函数行为会随调用方返回 nil 还是 `[]int{}` 意外变化。
- **容量提示**:
  - 切片：`make([]T, 0, len(m))` ——容量是**真实预分配**，append 在容量内零分配（`Keys` 函数模式）。
  - 映射：`make(map[string]os.FileInfo, len(files))` ——提示只是估计 hashmap bucket 数，不保证完全预分配，仍可能再分配。
  - **警告**: 大多数普通代码不需要 size 提示；预分配超过实际需要会浪费整个集群的内存甚至损害性能。
- **接口三规则**:
  1. **定义在使用侧而非实现侧**: 接口属于使用它的包；实现包返回具体类型（指针/结构体），便于将来加方法无需重构。不导出测试桩接口——消费者可自定义只含所需方法的接口；测试优先用真实实现（http 测试用 httptest 起本地服务）。不需要传不同类型就不用接口参数；遵循最小公开原则。
  2. **不用指向接口的指针**: 接口本身按值传递，底层实现可以用指针——`callGood(a Animal)` 而非 `callBad(pa *Animal)`。
  3. **编译期验证实现**: 指针/切片/映射类型用 `var _ IType = (*impl)(nil)`；结构体类型用 `var _ IType = impl{}`。
- **泛型使用原则**: 允许用，但传统方法（切片/映射/接口）同样有效且不增复杂性时优先传统方法。
  - 导出泛型 API 要记录使用泛型的缘由，强烈建议附可运行示例。
  - 只有一种类型可用时，先让代码在该类型上工作——**先删不必要的抽象，之后再加多态比反过来容易**。
  - 不用泛型发明 DSL，尤其不要引入复杂错误处理框架或断言库——遵从 Go 原生错误处理。
  - 总则：重在编写代码而非设计类型系统；多种类型有统一接口时先用接口建模（可能不需要泛型）；不依赖 `any` + 大量类型切换——类型间无共享接口且需处理多类型时才考虑泛型。
- **any**: Go 1.18 引入，是 `interface{}` 的别名。新代码用 `any` 替换 `interface{}`。

## Anti-patterns
- **区分 nil/空切片的 API**: `if s == nil { return }` 导致行为不确定性。
- **无容量提示的 map 构建**: 已知元素数却 `make(map[K]V)` 再循环插入。
- **生产者导出接口**: `package producer; type Thinger interface {...}; func NewThinger() Thinger` ——接口应留在 consumer 包。
- **为通用而泛型**: 不关心元素类型就上泛型、用泛型造 DSL/断言框架。

## Code Examples
```go
// Good: 编译期验证接口实现
var _ Animal = (*cat)(nil)

// Good: 切片预分配
s := make([]string, 0, len(m))
for k := range m {
    s = append(s, k)
}
```
- **What it demonstrates**: 接口实现的静态保证；已知规模集合的零分配构建。

## Key Takeaways
1. 空切片用 nil 初始化；判断用 len 不用 nil；接口不区分 nil/空切片。
2. 已知规模就给 make 容量：切片=真预分配，映射=bucket 估计。
3. 接口三件套：使用侧定义、不传接口指针、`var _ I = (*T)(nil)` 验证。
4. 泛型是最后手段：接口能建模就不泛型，单类型先用起来，不造 DSL。
5. 新代码 `any` 替代 `interface{}`。

## Connects To
- **Ch 8 字面值格式化**: 切片/映射字面量的书写格式。
- **Ch 25 测试结构体**: 表驱动测试与 fake 实现的落地。
