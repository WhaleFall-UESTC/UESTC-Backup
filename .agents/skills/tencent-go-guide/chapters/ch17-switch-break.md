# 第 17 章: switch 和 break

## Core Idea
Go 的 switch 自动中断——无标签的 `break` 是多余的；空子句用注释说明意图；跳出的对象是外层 for 时用标签 break。

## Frameworks Introduced
- **冗余 break 禁止**: Go switch 子句自动中断，C 风格穿透需要显式 `fallthrough`。空 case 说明意图用注释（`// handled outside of the switch statement`）而非 `break`。
- **break 语义**: switch 内无标签 `break` 只退出 switch 不退出循环。跳出外层循环用标签：`loop: for {...; switch {...; case "A": break loop}}`。

## Anti-patterns
- **case 末尾裸 break**: 冗余噪音。
- **误以为 switch 内 break 退出 for**: 需要 `break loop` 标签。

## Code Examples
```go
// Good: 空子句用注释
switch x {
case "C":
    // handled outside of the switch statement
default:
    return fmt.Errorf("unknown value: %q", x)
}

// Good: 标签 break 跳出循环
loop:
for {
    switch x {
    case "A":
        break loop // exits the loop
    }
}
```
- **What it demonstrates**: 注释替代冗余 break；标签 break 的正确退出目标。

## Key Takeaways
1. switch 子句不写 break；空 case 写注释。
2. switch 里的 break 只管 switch；跳出 for 用 `break <label>`。

## Connects To
- **Ch 11 条件与循环**: switch/case 单行格式规则。
