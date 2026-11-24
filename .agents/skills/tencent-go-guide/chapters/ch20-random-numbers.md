# 第 20 章: 正确使用随机数

## Core Idea
math/rand 是线性同余伪随机数：种子初始化一次（go1.20+ 自动）；取值用 `Intn(n)` 不用 `Int()%n`；概率边界防差一；高频场景自建加锁 Source；安全场景只用 crypto/rand。

## Frameworks Introduced
- **基本原理**: `x(i+1) = A * x(i) % M`（Lehmer 1951 线性同余）。种子 x(0) 不能为 0；M 取素数 2³¹-1、A 取 48271（Go math/rand 的选择）时周期最大 M-1。
- **种子初始化一次**: `rand.Seed` 已弃用；**go1.20+ 全局生成器自动随机初始化**，无需 Seed。老版本在 `init()` 里 Seed 一次。**禁止每次调用都 `rand.Seed(time.Now().UnixNano())`**: ① time.Now() 不廉价；② 同一纳秒内产生完全相同的序列。
- **均匀分布**: 用 `rand.Intn(n)` / `rand.Int31n(n)` / `rand.Int63n(n)` 取 [0,n)。**禁止 `rand.Int() % n`** —— n 不整除范围上限+1 时分布偏移。
- **差一错误**: `rand.Intn(100) < 80` 是 80% 概率；写成 `<= 80` 就变 81%——区间 [0,100) 含 100 个值，边界运算符要数清楚。
- **线程安全与性能**: 全局 rand 方法线程安全但高频调用锁竞争大。高频场景 `r := rand.New(rand.NewSource(seed))` 自建生成器——**它不是并发安全的**，需自加锁封装（`SafeRand{r *rand.Rand; mu sync.Mutex}` 模式）。
- **安全场景**: math/rand 可被预测，**绝不用于安全敏感随机数**（砍价金额、token、密钥）。用 `crypto/rand`: `rand.Int(rand.Reader, big.NewInt(100))`。

## Anti-patterns
- **每次调用重新 Seed**: 性能损耗 + 同纳秒同序列。
- **`rand.Int() % n`**: 分布不均匀。
- **`Intn(100) <= 80`**: 差一错误（81%）。
- **安全用途 math/rand**: 可预测 → 被攻击。

## Code Examples
```go
// Good: go1.20+ 直接用全局生成器
func isEven() bool { return rand.Intn(2) == 0 }

// Good: 安全场景 crypto/rand
n, _ := rand.Int(rand.Reader, big.NewInt(100))
```
- **What it demonstrates**: 默认全局生成器；crypto/rand 的 Int + big.Int 签名。

## Key Takeaways
1. go1.20+ 不用 Seed；老版本 init 里 Seed 一次。
2. `[0,n)` 一律 Intn 族，永不取模。
3. 概率 p% 用 `Intn(100) < p`，严守边界。
4. 高频随机 = 自建 Source + 互斥锁；安全随机 = crypto/rand。

## Connects To
- **Ch 22 常用库**: sync.Mutex 封装模式同源。
