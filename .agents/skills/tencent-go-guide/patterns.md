# Patterns — 腾讯 Go 语言编程指南

## Options Struct（封装结构体）
**When to use**: 所有调用者都需指定某些选项；不同调用者参数差异大；选项跨多个函数共享。
**How**: 参数打包为结构体作为最后一个参数；ctx 不进结构体；字段名即文档。
**Trade-offs**: 调用点冗长但自说明；加字段不破坏调用方。vs functional options。

## Functional Options（可变参数选项）
**When to use**: 大多数调用者不配置；option 很少用/很多/带参/可能失败/需大量文档/允许第三方扩展。
**How**: `type XOption func(*xOptions)` + 导出构造器返回闭包；默认值用 `DefaultXOptions` 切片先应用。
**Trade-offs**: 零配置零开销、可扩展；实现样板多。

## Guard Clause（卫语句/缩短错误路径）
**When to use**: 任何错误检查；替代 err==nil 嵌套金字塔。
**How**: `if err != nil { return err }` 顺序排列，正常路径零缩进。嵌套 ≤4 层。
**Trade-offs**: 无——除非正常路径需要共享清理。

## 哨兵值 + errors.Is
**When to use**: 调用者需要区分错误条件。
**How**: `var ErrXxx = errors.New(...)`；调用方 `errors.Is(err, ErrXxx)`（兼容包装）。需携带数据时定义结构化错误类型。
**Trade-offs**: vs %w 包装——%w 扩大 API 表面，需文档+测试才用，且放末尾。

## nil 切片 + len 判断
**When to use**: 声明空切片；接口接受切片参数。
**How**: `var t []string`；判断用 `len(s) == 0`；API 不区分 nil/空切片。
**Trade-offs**: 零成本；禁掉一类行为不确定性 bug。

## make 容量预分配
**When to use**: 已知元素数量（map 构建、Keys 类函数）。
**How**: 切片 `make([]T, 0, n)`（真预分配）；映射 `make(map[K]V, n)`（bucket 估计）。不要过量预分配。
**Trade-offs**: 性能敏感代码收益大；普通代码不需要。

## 编译期接口验证
**When to use**: 任何类型实现接口时。
**How**: `var _ IType = (*impl)(nil)`（指针/切片/映射接收器）或 `var _ IType = impl{}`（值结构体）。
**Trade-offs**: 一行换静态保证。

## 接口定义在使用侧
**When to use**: 定义接口时（默认姿势）。
**How**: 接口放 consumer 包；producer 返回具体类型；测试桩由消费者按需定义最小接口；http 集成测试用 httptest。
**Trade-offs**: 最小公开原则；实现加新方法无需重构。

## Must* 初始化模式
**When to use**: 程序启动早期失败的解析/加载（包级变量初始化）。
**How**: `func MustParse(s string) *T { v, err := Parse(s); if err != nil { log.Fatalf(...) }; return v }`；`var Default = MustParse(...)`。
**Trade-offs**: 失败即退出不可恢复——运行时禁用。

## ctx 生命周期管理
**When to use**: 任何 goroutine / 长任务。
**How**: `go process(ctx, item)`；process 至迟在 ctx 取消时返回；同步代码限制在函数范围。
**Trade-offs**: vs 裸 `go process(item)` ——后者无法优雅停止、难测试。

## DeepCopy 显式拷贝
**When to use**: 确需拷贝含内部引用/锁的类型（Buffer、Mutex）。
**How**: 新建底层存储 + copy + 构造新对象；含此类字段的结构体参数/返回值/接收器全指针化。
**Trade-offs**: 显式成本 vs 隐式别名 bug。

## SafeRand 封装
**When to use**: 高频随机数生成。
**How**: `rand.New(rand.NewSource(seed))` + `sync.Mutex` 方法封装；注意 rand.Rand 本身非并发安全。
**Trade-offs**: 消除全局锁竞争；需自己保证安全。安全场景改用 crypto/rand。

## t.Helper + must* 测试辅助
**When to use**: setup/清理辅助函数。
**How**: `t.Helper()` 第一行；setup 失败直接 `t.Fatalf("Setup failed: ...")`；清理返回 func 或 t.Cleanup；不产生失败的辅助不收 *testing.T。
**Trade-offs**: 失败行号归属调用处；误用于断言辅助会破坏模式。

## 表驱动 + 子测试 + t.Fatal
**When to use**: 多用例共享检查逻辑。
**How**: `t.Run(d.name, func(t *testing.T){...})`；子测试内 t.Fatal；行内无环境分支；用例名如函数名、无斜杠。
**Trade-offs**: 过滤友好、失败隔离；逻辑不同的用例拆成多个测试函数。

## 真实传输协议集成测试
**When to use**: RPC/HTTP 组件集成测试。
**How**: 真实 client 连接测试版 server（httptest 等）；优先被测服务官方测试库。
**Trade-offs**: 模仿 client 正确性复杂；真实组件让测试更接近生产。
