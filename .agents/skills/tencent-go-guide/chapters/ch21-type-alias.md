# 第 21 章: 类型别名

## Core Idea
`type T1 T2` 定义新类型；`type T1 = T2` 是别名——别名很少见，主要用于包迁移到新位置，通常不需要使用。

## Frameworks Introduced
- **类型定义 vs 类型别名**: 定义（`type T1 T2`）创建全新类型（有自己的方法集）；别名（`type T1 = T2`）只是引用现有类型的另一个名字。`any` 就是 `interface{}` 的别名（见第 7 章）。

## Key Takeaways
1. 默认用类型定义；类型别名留给渐进式包迁移场景。

## Connects To
- **Ch 7 内置类型**: `any` 别名是别名的主要现代用例。
