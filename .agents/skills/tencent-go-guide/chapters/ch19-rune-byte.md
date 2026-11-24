# 第 19 章: string 处理中的 rune 和 byte 混淆

## Core Idea
`byte`=uint8（字节/ASCII），`rune`=int32（Unicode 码点）；Go string 是 UTF-8 字节序列——`len` 和下标都是字节层次，中文字符会让两者失真，需要 `[]rune` 转换。

## Frameworks Introduced
- **UTF-8 前缀格式**: `0xxxxxxx` 单字节（U+0000-007F）；`110xxxxx 10xxxxxx` 双字节（U+0080-07FF）；`1110xxxx 10xxxxxx 10xxxxxx` 三字节（U+0800-FFFF）；`11110xxx 10xxxxxx ×3` 四字节（U+10000-10FFFF）。汉字（如 腾 U+817E、讯 U+8BAF）是三字节字符。
- **byte vs rune 层次**: byte 处理字节序列，rune 处理字符序列，都可用于 UTF-8 string。
- **非 ASCII 时 len 陷阱**: `len("腾讯PCG")` = 9（字节）而非 5（字符）——非 ASCII 字符占多字节，len 返回值 ≠ 打印字符数。

## 三种遍历对照（"腾讯PCG"）
| 写法 | 下标 | 内容 | 问题 |
|---|---|---|---|
| `for i, v := range name` | 0,3,6,7,8（不连续） | 正确 | 下标是字节偏移，下标相关逻辑出错 |
| `name[i]` 下标遍历 | 0-8 连续 | `è ¾ è ® ¯ P C G` 乱码 | 字节被逐个打印 |
| `[]rune(name)` 后下标遍历 | 0-4 连续 | 腾讯PCG 正确 | ✅ |

**规则**: 字符串可能含中文且逻辑依赖下标或内容时，先转 `[]rune`。
- `for range` 不转 rune 时：内容对（按 rune 解码）但下标是字节偏移。
- 下标遍历不转 rune 时：下标对但内容是 UTF-8 字节碎片（乱码）。

## Code Examples
```go
// Good: 中文相关处理先转 []rune
name := []rune("腾讯PCG")
for i := 0; i < len(name); i++ {
    fmt.Printf("%d: %c\n", i, name[i]) // 0:腾 1:讯 2:P 3:C 4:G
}
```
- **What it demonstrates**: rune 切片让下标与内容同时正确。

## Key Takeaways
1. `len(s)` 与 `s[i]` 是字节层次；`range s` 是 rune 层次但下标为字节偏移。
2. 含中文的字符串需要下标对齐时先 `[]rune()`。
3. 汉字 = 3 字节 UTF-8（U+0800-FFFF 段）。

## Connects To
- **Ch 18 字符串拼接**: 字符串操作的另一面。
