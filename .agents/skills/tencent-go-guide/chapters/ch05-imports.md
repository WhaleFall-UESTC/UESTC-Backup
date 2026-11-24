# 第 5 章: 导入

## Core Idea
导入默认不重命名；只在冲突时重命名，且优先重命名本地/项目特定包；空白导入限 main 与测试，点导入全面禁用。

## Frameworks Introduced
- **导入重命名**: 仅当冲突时使用（好包名应不需要重命名）。冲突时优先重命名**本地包或项目特定的第三方依赖**。重命名遵循包命名规范（无下划线、无大写）。包名无标识信息时（如 `v1`）带上层目录信息：`core ".../api/core/v1"`。与本地变量重名时可加 `pkg` 后缀（`url` 变量冲突 → `urlpkg`）；不建议依赖"局部变量遮蔽包名"——Go 允许但会造成混淆，重命名其一。
- **空白导入 `import _`**: 仅允许 main 包和测试代码。副作用：依赖关系不受控，可能隐藏依赖冲突。
- **点导入 `import .`**: 任何包都不建议使用——无法判断逻辑来源，无法区分包内定义与外部依赖。
- **Proto 导入约定**: 基于 codegen 规则用前缀——`pb`（go_proto_library）、`trpc`（go_trpc_library）；一般用 1-2 字母前缀（`fspb`、`fsgrpc`）；只用一个 proto 或紧密相连时可省略前缀（`pb`）；符号通用或前缀不自描述时用短单词（`mapspb`——`mapspb.Address` 比 `mpb.Address` 清晰）。

## Anti-patterns
- **无冲突使用别名**: 无冲突不应重命名导入。
- **非 main/测试代码使用 `import _`**: 依赖失控。
- **任何场景使用 `import .`**: 来源不清。

## Code Examples
```go
// Good: v1 无标识信息，带上层目录
import (
    core "github.com/kubernetes/api/core/v1"
    fspb  "path/to/package/foo_service_go_proto"
)
```
- **What it demonstrates**: 别名要传递路径/组件信息，不是随便缩写。

## Key Takeaways
1. 重命名 = 冲突时才用，优先本地包，保持小写无下划线。
2. `import _` 限 main + 测试；`import .` 全禁。
3. proto/gRPC 导入用带语义的短前缀（`fspb`/`fsgrpc`/`mapspb`）。

## Connects To
- **Ch 2 命名**: 包命名规范是导入别名的基础。
