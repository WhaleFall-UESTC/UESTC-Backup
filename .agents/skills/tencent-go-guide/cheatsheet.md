# Cheatsheet — 腾讯 Go 语言编程指南速查

## 命名决策
| 场景 | 规则 | 例 |
|---|---|---|
| 包名 | 小写单词、单数、无 util/common/helper | `bufio`、`strconv` |
| 接口 | 单方法 + `-er` 后缀 | `Reader`、`Formatter` |
| 接收器 | 1-2 字母、全类型一致 | `func (c *Calculator)` |
| 取值器 | 无 Get 前缀 | `Counts()` 不是 `GetCounts()` |
| 变量长度 | ∝ 作用域（小1-7行/中8-15/大15-25/超大>25） | 小作用域 `n`，大作用域 `userCount` |
| 冗余消除 | 依次检查：包名/接收器/参数/返回值/类型 | `db.New()` 不是 `db.NewDB()` |
| 函数命名 | 返回结果→名词；做操作→动词；仅类型不同→尾部加类型 | `JobName` / `WriteDetail` / `ParseInt64` |

## 错误处理决策树
- 调用者需要区分错误？→ 哨兵值 `ErrXxx` + `errors.Is`（永不 regexp 字符串）
- 需要携带数据？→ 结构化错误类型（如 `PathError{Op, Path, Err}`）
- 包装加上下文？→ 默认 `%v`；**仅当**文档化+测试暴露底层错误时用 `%w`，且放末尾 `[...]: %w`
- 错误信息写什么？→ 出了什么问题 + 出错值 + 要求/约束 + 怎么修复/示例；首字母小写、无结尾标点
- 错误出现位置？→ 立即返回（guard clause）；初始化错误传到 main 由 `log.Fatal` 报告
- 测试断言错误？→ 测 `err != nil` 存在性或 `cmpopts.EquateErrors`；不测错误字符串

## panic / Fatal 边界
| 信号 | 动作 |
|---|---|
| 业务/瞬态错误 | 返回 error |
| 无效配置（启动期） | `log.Fatal`（注意：defer 不执行） |
| API 误用/不变量破坏（不可恢复） | panic（标准库风格） |
| 包内部深层递归的错误传递 | panic + 顶层 recover，不跨包边界 |
| `log.Fatal` 后的不可达分支 | 补 `panic("unreachable")` |
| 启动早期解析失败 | `MustXxx` + `log.Fatalf` |

## 阈值与默认值
- 嵌套深度 ≤ **4 层**；注释列宽 ≤ **120 列**；小作用域 ≈ **1-7 行**
- 简单字符串拼接 `+` 栈分配阈值：结果 ≤ **32 字节** 且不逃逸
- 函数参数失控阈值：>3-4 个 → Options struct 或 functional options
- 接收器拿不定主意 → **指针**；同一类型方法全指针或全值
- ctx 永远第一个参数；flag 名蛇形、变量名驼峰

## 工具选择速查
| 需求 | 用 |
|---|---|
| 拼几个字符串 | `+` |
| 格式化 | `fmt.Sprintf`；写 Writer 用 `fmt.Fprintf` |
| 循环构建大字符串 | `strings.Builder`（O(n)） |
| 带分隔符连接 | `strings.Join` |
| 常量/多行字符串 | 反引号原始字面量 |
| 文件路径 | `filepath.Join`（跨平台+防注入） |
| 结构体比较（测试） | `cmp.Equal` / `cmp.Diff` |
| JSON 解码 | 结构体，不 map[string]interface{} |
| 安全随机数 | `crypto/rand`；其他 `rand.Intn(n)`（永不 `Int()%n`） |
| 子命令 CLI | cobra，ctx 用 `cmd.Context()` |
| HTTP 响应 | `defer resp.Body.Close()` 紧跟 err 检查 |
| 静态资源 | `//go:embed`（路径相对 Go 文件） |
| 静态检查 | `go vet` + `Staticcheck` |

## 接收器类型决策
需要修改接收者 / 含 sync.Mutex / 含指向可变内容的指针字段 / 大结构 → **指针**
map / chan / func / 内置类型（不修改）/ 小值类型 / 切片不重切 → **值**

## 坏味道速查（看见就该改）
- `util`/`common`/`helper` 包名 → 按调用方语义改名
- `GetCounts()` → 去掉 Get
- `regexp` 匹配 `err.Error()` → 哨兵值/结构化错误
- `else { 正常代码 }` 跟在错误检查后 → 卫语句提前 return
- `err == nil` 作主路径 → `err != nil`
- 表行里放环境枚举 → 拆成两个测试函数
- `failed on case #%d` → 打印用例名和输入
- goroutine 里调 `t.Fatal` → `t.Errorf` + return
- `t.Fatal` 消息没有 Setup 上下文 → 加 `"Setup failed: ..."`
- `"foo" == result`（Yoda）→ 变量在左
- 循环里 `rand.Seed(time.Now()...)` → 删掉（go1.20+ 自动种子）
- 每次调用重新 make 没有容量提示的 map → `make(map, len(src))`
- struct 里的 ctx 字段 → 改成方法第一参数
- go<1.22 闭包捕获循环变量 → `v := v` 或传参

## 四原则冲突裁决（按序）
逻辑清晰 > 简单 > 简洁 > 可维护性；一致性冲突时**以规范为准**，旧代码迁移到规范。
