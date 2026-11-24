# 第 23 章: 复杂的命令行界面

## Core Idea
子命令式 CLI（kubectl 风格）用 cobra；cobra 命令函数用 `cmd.Context()` 取上下文，不要自建 `context.Background()`。

## Frameworks Introduced
- **cobra 选型理由**: 社区广泛使用、getopt 风格标志、功能丰富。
- **cobra 陷阱**: 命令函数应使用 `cmd.Context()` 获取上下文，而非自己 `context.Background()` 创建根上下文。
- **包组织**: 子命令不必每条一个包（Go 代码库处处要考虑包边界）。
- **CLI 与库分离**: 代码既能作库又能作二进制时，把 CLI 处理代码与库功能代码分开——CLI 成为库的另一个客户端，库可被其他程序复用而不受 CLI 干扰，模块化且易维护。

## Key Takeaways
1. 子命令 CLI → cobra；ctx 从 cmd.Context() 来。
2. CLI 代码与库代码分包，库不依赖 CLI。

## Connects To
- **Ch 22 常用库**: flag 包适合简单场景，cobra 适合子命令场景。
- **Ch 1 基本原则**: 不过度分包的边界——CLI/库分离是功能边界而非仪式。
