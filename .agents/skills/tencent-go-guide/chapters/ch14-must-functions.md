# 第 14 章: Must 函数

## Core Idea
失败即退出程序的辅助函数命名 `MustXxx`/`mustXxx`，只用于程序启动早期（包级变量初始化）；日常运行一律走常规 error 流程。

## Frameworks Introduced
- **Must 命名规范**: `MustParse`、`template.Must`、`regexp.MustCompile`——失败时 `log.Fatalf` 并带出错的输入值（`MustParse(%q) = _, %v`）。
- **包级"常量"初始化**: `var DefaultVersion = MustParse("1.2.3")` ——想要包级常量但又必须经过解析时，用 Must 函数在包初始化时求值，避免在 `init` 里赋值。

## Code Examples
```go
func MustParse(version string) *Version {
    v, err := Parse(version)
    if err != nil {
        log.Fatalf("MustParse(%q) = _, %v", version, err)
    }
    return v
}
var DefaultVersion = MustParse("1.2.3")
```
- **What it demonstrates**: Must 封装 + 包级变量初始化的标准姿势。

## Key Takeaways
1. `MustXxx` = 失败即 Fatal，仅限启动早期/包级初始化。
2. 运行时代码必须返回 error 走常规处理。

## Connects To
- **Ch 6 错误处理 / Ch 13 避免 Panic**: Must 内部用 log.Fatal 而非 panic。
