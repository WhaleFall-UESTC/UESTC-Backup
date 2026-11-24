# 第 18 章: 字符串拼接

## Core Idea
按场景选拼接方式：简单用 `+`，格式化用 `fmt.Sprintf`，循环构建用 `strings.Builder`，分隔符用 `strings.Join`，路径用 `filepath.Join`。

## Frameworks Introduced
- **决策规则**:
  1. **连接几个字符串 → `+`**: 语法最简、无需导入；结果 ≤32 字节且不逃逸时在栈上分配。
  2. **格式化字符串 → `fmt.Sprintf`**: 多个 `+` 拼接含 `strconv.Itoa` 的表达式晦涩难读。**写入 io.Writer 时不要先 Sprintf 再 Write——直接 `fmt.Fprintf(w, ...)`** 免去临时字符串。格式化再复杂时选 `text/template` / `safehtml/template`。
  3. **循环逐步构建大字符串 → `strings.Builder`**: Builder O(n)，`+`/`Sprintf` O(n²)。循环内 `fmt.Fprintf(b, ...)` 也行（Builder 实现 io.Writer）。
  4. **常量/多行字符串 → 反引号 `` ` ``** 原始字符串字面量，不用 `"" +` 拼接 + `\n`。
  5. **插入分隔符 → `strings.Join`**: 预分配结果内存，优于 `+=`；海量拼接仍考虑 Builder。
  6. **文件路径 → `filepath.Join`**: 手拼 `dir + "/" + sub` 有跨平台问题 + 外部输入时的命令注入风险。
- **可选方法清单**: `+`、`fmt.Sprintf`、`strings.Builder`、`text/template`、`safehtml/template`、`strings.Join`。

## Code Examples
```go
// Good: 循环构建 O(n)
b := new(strings.Builder)
for i, d := range digitsOfPi {
    fmt.Fprintf(b, "the %d digit of pi is: %d\n", i, d)
}
str := b.String()

// Good: 路径拼接
path := filepath.Join(dir, subdir, "file.txt")
```
- **What it demonstrates**: Builder + Fprintf 组合；路径安全拼接。

## Key Takeaways
1. 记住六场景六工具的对应关系，特别是 Fprintf 直写 Writer。
2. 循环拼接性能：Builder ≫ Join > + / Sprintf。
3. 路径永远 filepath.Join（跨平台 + 注入防护）。

## Connects To
- **Ch 19 rune/byte**: 字符串内容的字节层次问题。
- **Ch 1 基本原则**: 最少依赖——先标准库。
