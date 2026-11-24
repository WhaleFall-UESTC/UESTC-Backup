# 第 12 章: 拷贝

## Core Idea
含不可复制字段（`sync.Mutex`、内部切片引用等）的类型禁止值拷贝——类型 `T` 的方法与 `*T` 关联时，不要拷贝 `T` 的值。

## Frameworks Introduced
- **别名问题**: 复制 `bytes.Buffer` 后副本的切片可能引用原数组，后续方法调用产生异常。同步对象（`sync.Mutex`）绝不能复制。
- **DeepCopy 模式**: 确需拷贝时封装显式拷贝函数——`make` 新底层数组 + `copy` + `bytes.NewBuffer`。
- **API 防拷贝设计**: 结构体含不可复制字段时，参数与返回值都用指针类型——`func New() *Record`、`func (r *Record) Process(...)`、`func Consumer(r *Record)`。值接收器方法会在调用时隐藏复制过程（`func (r Record) Process` 复制了 `r.buf`）。

## Anti-patterns
- **`b2 := b1`（Buffer/Mutex）**: 别名 + 锁状态复制。
- **不可复制类型用值接收器/值参数**: 每次调用隐式拷贝。

## Code Examples
```go
// Good: 显式深拷贝
func DeepCopy(b *bytes.Buffer) *bytes.Buffer {
    newBytes := make([]byte, b.Len())
    copy(newBytes, b.Bytes())
    return bytes.NewBuffer(newBytes)
}
```
- **What it demonstrates**: 拷贝要显式、要复制底层存储而非引用。

## Key Takeaways
1. `*T` 有方法 → 不要拷贝 `T` 值（Buffer、Mutex、sync 族全部适用）。
2. 含不可复制字段的 API 全指针化：构造器、方法接收器、参数、返回值。
3. 真需要拷贝就写 DeepCopy，别依赖结构体赋值。

## Connects To
- **Ch 16 接收器类型**: 值/指针接收器选择的完整规则。
- **Ch 22 常用库**: sync.Mutex 的正确用法。
