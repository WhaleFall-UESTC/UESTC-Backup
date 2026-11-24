# 第 2 章: 命名

## Core Idea
命名首要符合代码规范；核心思路是**利用包名/接收器/上下文消除冗余**——好名字往往比你想的更短。

## Frameworks Introduced
- **包名规则**: 简短、唯一、全小写无下划线不驼峰、单数、可用惯用缩写（`rand`、`io`、`fmt`）；与目录名一致；用包名引用内容（`bufio.Reader` 而非 `BufReader`）。禁止 `common`、`util`、`helper` 等无信息量名称。
  - 构造函数：包导出唯一类型时可只叫 `New`（`ring.New`）。
  - 长命名不增加可读性：`once.Do` 优于 `Once.DoOrWaitUntilDone`；必要时加文档注释胜过冗长名称。
- **接口名**: 单方法接口加 `-er` 后缀（`Reader`、`Writer`、`Formatter`）。不要占用 `Read`/`Write`/`Close`/`String` 等规范名，除非签名和语义相同——字符串转换方法叫 `String()` 不叫 `ToString()`。
- **接收器名**: 极短（1-2 个字母）、类型缩写、**每个类型的所有方法保持一致**（`func (c *Calculator) Add` / `Sub`，不要一会儿 `cal` 一会儿 `calculator`）。
- **取值器名**: 不用 `Get` 前缀，直接名词开头——`Counts()` 而非 `GetCounts()`；setter 用 `SetXXX`（`SetOwner`）。
- **变量名长度法则**: 长度与作用域大小成正比，与使用次数成反比。
  - 作用域分级：小 1-7 行、中 8-15 行、大 15-25 行、超大 >25 行。
  - 例外：`db` 这类无歧义的常见缩写可以在大作用域保持短名。
- **减少重复（四层去冗余）**:
  1. 包名与导出标识符：`db.NewDB()` → `db.New()`；`charcounter.CountChar()` → `charcounter.Count()`
  2. 方法名不重复接收器：`WriteTo(w)` 而非 `WriteConfigTo(w)`
  3. 函数名不重复参数：`Override(dest, source)` 而非 `OverrideFirstWithSecond`
  4. 函数名不重复返回值：`Transform(input)` 而非 `TransformYAMLToJSON(input)`（调用处已有上下文）
  5. 变量名不重复类型：`var users []*User` 而非 `userSlice`；`userCount` 而非 `numUsers`/`usersInt`

## Key Concepts
- **变量名反映内容与用途**: 反映"包含什么、在当前上下文干什么用"，而不是值的来源。最佳局部变量名通常与结构体/protobuf 字段名不同。
- **类型限定词**: 仅当同作用域存在同一对象的两种类型时才加类型后缀（`limitStr` + `limit`；两个都有时 `Raw` 不如 `Str` 直观）。
- **映射命名**: 键值对用 `usersByID` 表明结构；上下文清晰时集合也用复数 `users`。
- **函数命名约定**: 返回结果用名词（`JobName`）；执行操作用动词（`WriteDetail`）；仅返回类型不同的同类函数在末尾加类型（`ParseInt`/`ParseInt64`/`AppendInt`）；有明确"主要"版本时可省略类型（`Marshal` + `MarshalText`）。
- **工具包命名**: 从调用方视角检查——`elliptic.Marshal(curve, x, y)` 不看 import 也能懂；`helper.Marshal(...)` 则不行。

## Anti-patterns
- **`util`/`common`/`helper` 包**: 无信息量、难以阅读、易导致导入冲突。
- **`GetCounts()`**: Go 取值器不加 Get 前缀。
- **接收器名不一致**: 同一类型的不同方法用不同接收器名。
- **为省字删字母**: `Sbx` 劣于 `Sandbox`（导出名尤其如此）。
- **无谓的类型后缀**: `userString`、`userSlice`、`numUsers`。

## Code Examples
```go
// Good: 取值器直接用名词
func (c *Counter) Counts() int { return c.counts }

// Good: 短接收器名 + 全类型一致
func (c *Calculator) Add(a, b int) int { /* ... */ }

// Good: 同作用域两种类型才加限定词
limitStr := r.FormValue("limit")
limit, err := strconv.Atoi(limitStr)
```
- **What it demonstrates**: Go 命名靠上下文而非前缀/后缀堆砌信息。

## Key Takeaways
1. 命名的冗余消除顺序：包名 → 接收器 → 参数 → 返回值 → 类型，每层都可让名字更短。
2. 接收器名 1-2 字母且全类型一致；接口用 `-er` 后缀；取值器不加 `Get`。
3. 变量名长度 ∝ 作用域；单字母仅用于小作用域且惯例明确（`r` = Reader/Request，`x`,`y` = 坐标）。
4. 禁用 `util`/`common`/`helper` 包名；判断标准是"不看 import 能否看懂调用"。

## Connects To
- **Ch 3 注释**: 名字表达不了的信息用文档注释补充，而非加长名字。
- **Ch 16 接收器类型**: 接收器命名之外的选择（指针 vs 值）。
