# 第 8 章: 字面值格式化

## Core Idea
gofmt 之上还要人为追求可读性：结构体字面量是否标字段名取决于来源包与复杂度；大括号同行成对匹配；零值字段省略——表驱动测试除外。

## Frameworks Introduced
- **结构体初始化字段名**: 外部包的结构体初始化**必须**标字段名；包内简单结构体可省略（如 `image.Point{42, 54}`，其组成与顺序文档化且稳定）；复杂本地结构体仍要标字段名。
- **大括号规则**: 尽量同一行内成对匹配。多行列表元素 `{...}` 每个元素独立成对；混合字面量与变量时变量不能 cuddle；proto Builder 可 cuddle 省纵向空间。
- **零值字段省略**: 设计良好的 API 采用零值可用结构——省略零值字段让读者注意真正指定的选项（`BlockSize` + `ErrorIfDBExists`，其余 8 个零值字段全部省略）。
  - **例外: 表驱动测试** —— 显式赋值零值让测试预期一目了然（`wantIPv4: false` 不省略）。

## Anti-patterns
- **外部结构体按位置初始化**: `csv.Reader{',', '#', 4, false, false, false, false}` ——字段含义全靠数。
- **零值字段逐一写出**: 噪音淹没真正的配置意图（表驱动测试除外）。
- **列表末尾元素大括号挤在前一行**: `{Key: "line"}}` 闭合混乱。

## Code Examples
```go
// Good: 外部类型标字段名
ldb := leveldb.Open("/my/table", &db.Options{
    BlockSize:       1 << 16,
    ErrorIfDBExists: true,
})

// Good: 表驱动测试显式写零值（例外规则）
tests := []struct {
    input    string
    wantIPv4 bool
    wantErr  bool
}{
    {input: "hostname", wantIPv4: false, wantErr: true},
}
```
- **What it demonstrates**: 只写非零配置 + 测试用例显式预期。

## Key Takeaways
1. 外部/复杂结构体标字段名；包内简单结构体（Point 等）可按位置。
2. 零值字段省略，但表驱动测试要显式写。
3. 大括号成对同行匹配，元素独立成对。

## Connects To
- **Ch 25 测试结构体**: 表驱动测试完整写法。
