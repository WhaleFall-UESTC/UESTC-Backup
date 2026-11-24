# Kotlin 编码规范精要

> 基于主流 Kotlin 编码规范浓缩整理

---

## 一、变量与不可变性

### 核心原则
- **优先使用 `val` 而非 `var`**，减少可变状态
- 最小化变量作用域
- 对外暴露只读集合类型（`List`、`Set`、`Map`），而非可变类型

### 关键规则
1. `val` 用于引用不可变；若对象内容需变化，也优先用 `val`
2. 变量应在最小使用范围内声明，避免类级别的共享状态
3. 高开销初始化用 `by lazy`（数据库连接、文件读取、单例等）
4. `lateinit` 仅用于有明确生命周期保证的场景（如 Android `Activity.onCreate`）

### 禁止事项
- ❌ 使用 `lateinit` 配合 `isInitialized` 检查（应换用 nullable 或其他方案）
- ❌ 在 API 中直接暴露 `MutableList`/`MutableSet`/`MutableMap`
- ❌ 将只读集合强转为可变集合（可能抛 `UnsupportedOperationException`）

### 反例 → 正例
```kotlin
// ❌ 类级别可变状态 / 暴露可变集合
class UserProfile {
    var result = 0
    val hobbies = mutableListOf<String>()  // 暴露可变集合
}

// ✅ 方法内局部变量 / 封装可变性
class UserProfile {
    private val _hobbies = mutableListOf<String>()
    val hobbies: List<String> get() = _hobbies  // 暴露只读视图
}
```

---

## 二、函数

### 核心原则
- 使用 `require` 进行前置条件验证（抛 `IllegalArgumentException`）
- 使用 `check` 检验内部不变量（抛 `IllegalStateException`）
- 函数参数存在歧义时使用具名参数

### 关键规则
1. 多个同类型参数、布尔标志参数、长参数列表均应使用具名参数
2. 运算符重载行为必须与运算符语义一致
3. 高阶函数使用 `inline` 减少 lambda 分配开销；不需要内联的参数用 `noinline`
4. 使用 `crossinline` 防止 lambda 中的非本地返回
5. 注册/注销监听器时，必须保存监听器对象引用，不能用匿名 lambda 注销

### 禁止事项
- ❌ 同类型多参数未使用具名参数（语义不清）
- ❌ 运算符重载与运算符语义不符（如 `times` 不执行乘法）
- ❌ 用匿名 lambda 注销监听器（注销无效）

### 反例 → 正例
```kotlin
// ❌ 参数含义不明确
resizeImage(1024, 768, 1.5)

// ✅ 具名参数清晰表达意图
resizeImage(width = 1024, height = 768, scale = 1.5)

// ❌ 无法注销的匿名 lambda 监听器
eventSource.addListener { println("Event") }
eventSource.removeListener { println("Event") }  // 无效！不同对象

// ✅ 保存引用后再注销
val listener = EventListener { println("Event") }
eventSource.addListener(listener)
eventSource.removeListener(listener)  // 有效
```

---

## 三、类与对象

### 核心原则
- 简单对象优先使用主构造函数（带默认参数）
- 复杂构建逻辑使用工厂方法（companion object、顶层函数、伪构造函数）
- 状态带数据时用 `sealed class`，纯枚举用 `enum class`

### 关键规则
1. Kotlin 中一般无需 Builder 模式（用主构造函数 + 默认参数代替）
2. 属性代表状态，不应有外部依赖、昂贵计算或可能抛异常的 getter
3. 使用依赖注入处理有外部依赖的对象
4. 需要携带附加数据的状态枚举，用 `sealed class` 替代 `enum class`

### 禁止事项
- ❌ 在属性 getter 中调用网络/IO 操作
- ❌ 在属性 getter 中抛出异常（应改为方法）
- ❌ 用 Builder 模式构建 Kotlin 中可用主构造函数表达的简单对象

### 反例 → 正例
```kotlin
// ❌ getter 中有 IO 操作和异常
class WeatherService {
    val temperature: Double
        get() = fetchTemperatureFromAPI()  // 应该是方法，不是属性

    val profilePictureUrl: String
        get() = if (!isLoggedIn) throw IllegalStateException() else fetchUrl()
}

// ✅ 改为方法
class WeatherService {
    fun getTemperature(): Double = fetchTemperatureFromAPI()
    fun getProfilePictureUrl(): String? = if (isLoggedIn) fetchUrl() else null
}
```

---

## 四、协程（Coroutines）

### 核心原则
- 函数应尽量保持纯粹，避免访问共享可变状态和副作用
- 使用结构化并发（viewModelScope、rememberCoroutineScope、自定义 scope）
- 正确处理协程取消

### 关键规则
1. 取消时不要捕获/吞掉 `CancellationException`，必须重新抛出
2. CPU 密集型循环中加入取消检查点：`ensureActive()` 或 `yield()`
3. `suspendCancellableCoroutine` 必须实现 `invokeOnCancellation` 释放资源
4. 协程中使用 `Mutex`（可挂起锁）而非 `synchronized`（阻塞线程）
5. `CoroutineExceptionHandler` 仅对顶层协程和 `supervisorScope` 的直接子协程有效
6. 使用 `supervisorScope` 防止子协程异常取消父/兄弟协程

### 禁止事项
- ❌ 使用 `GlobalScope`（内存泄漏风险）
- ❌ 捕获 `Exception` 而吞掉 `CancellationException`
- ❌ CPU 密集型循环中无取消检查点（`withTimeout` 超时无效）
- ❌ 协程中使用 `synchronized` 阻塞线程
- ❌ Compose 中在 Composable 函数体内修改 State（导致无限重组）

### 反例 → 正例
```kotlin
// ❌ GlobalScope
GlobalScope.launch { doWork() }

// ✅ 结构化 scope
viewModelScope.launch { doWork() }

// ❌ 吞掉 CancellationException
try {
    delay(1000L)
} catch (e: Exception) {
    println("Caught")  // CancellationException 被吞掉！
}

// ✅ 重新抛出 CancellationException
try {
    delay(1000L)
} catch (e: CancellationException) {
    throw e  // 必须重新抛出
} catch (e: Exception) {
    println("Caught non-cancellation: ${e.message}")
}

// ❌ 协程中使用 synchronized（阻塞线程）
synchronized(lock) { doHardWork() }

// ✅ 使用可挂起 Mutex
val mutex = Mutex()
mutex.withLock { doHardWork() }

// ❌ CPU 循环无取消检查点
suspend fun longRunning() = withTimeout(100) {
    var i = 0
    while (true) { i++ }  // 永远不会超时！
}

// ✅ 加入取消检查点
suspend fun longRunning() = withTimeout(100) {
    var i = 0
    while (true) {
        ensureActive()
        i++
    }
}
```

---

## 五、异常处理

### 核心原则
- **禁止**将异常用作控制流工具
- 对预期失败，使用 `Result<T>`、sealed class 或 nullable 类型返回错误
- 捕获精确的异常类型，避免捕获宽泛的 `Exception`

### 关键规则
1. 优先使用 `xxxOrNull` 系列函数替代异常抛出（`toIntOrNull()`、`firstOrNull()`）
2. 可预期的失败（业务失败）使用 `Result<T>` 或 sealed class 表达
3. 使用 `require` 检查参数合法性，`check` 检查状态合法性
4. 在内部处理错误，不要将底层异常暴露给调用者（封装后再抛）
5. 不要在循环/`forEach` 中用异常实现中断逻辑（用 `firstOrNull` 等替代）

### 禁止事项
- ❌ 使用异常做正常控制流（如表单验证抛异常）
- ❌ 捕获 `Exception` 太宽泛（可能吞掉 `CancellationException`）
- ❌ 用 `catch (e: Exception)` 替代 `xxxOrNull` 函数
- ❌ 将底层 `RuntimeException` 直接传播到上层调用者

### 反例 → 正例
```kotlin
// ❌ 异常用作控制流
fun validateUser(email: String): User {
    if (email.isBlank()) throw IllegalArgumentException("Email empty")
}

// ✅ 返回结果类型
fun validateUser(email: String): Result<User> {
    if (email.isBlank()) return Result.failure(IllegalArgumentException("Email empty"))
    return Result.success(User(email))
}

// ❌ 解析失败靠异常
fun parseNumber(str: String): Int = str.toInt()

// ✅ 使用 OrNull 变体
fun parseNumber(str: String): Int? = str.toIntOrNull()

// ❌ forEach 中用异常中断
fun findFirstNegative(numbers: List<Int>): Int? {
    try {
        numbers.forEach { if (it < 0) throw RuntimeException() }
    } catch (e: RuntimeException) { return null }
    return null
}

// ✅ 直接用 firstOrNull
fun findFirstNegative(numbers: List<Int>): Int? = numbers.firstOrNull { it < 0 }
```

---

## 六、集合

### 核心原则
- 默认使用不可变集合
- 对外 API 始终暴露不可变集合
- 优先使用函数式操作（`map`、`filter`、`reduce`、`fold`）替代命令式循环

### 关键规则
1. 返回集合前调用 `toList()`/`toSet()` 确保返回不可变快照
2. `forEach` 中禁止使用无标签 `return`（会返回外层函数，而非跳过当前元素）
3. 需要类似 `continue` 效果时使用 `return@forEach`
4. 大数据集或有多个链式操作时，改用 `Sequence` 实现惰性求值，避免中间集合分配
5. 有早终止需求（`firstOrNull`、`any`、`all`）优先用 `Sequence`

### 禁止事项
- ❌ 返回 `MutableList` 类型的公共 API
- ❌ `forEach` 中使用无标签 `return`（语义歧义且危险）
- ❌ 大数据集上链式多次 `filter`/`map` 而不用 `Sequence`（产生大量中间集合）

### 反例 → 正例
```kotlin
// ❌ 暴露可变集合
fun getUsers(): MutableList<User> = users

// ✅ 返回只读接口
fun getUsers(): List<User> = users.toList()

// ❌ forEach 中无标签 return（退出函数而非跳过元素）
fun processNumbers(numbers: List<Int>) {
    numbers.forEach {
        if (it < 0) return  // ❌ 直接退出 processNumbers 函数！
        println(it)
    }
}

// ✅ 使用 return@forEach 跳过当前元素
fun processNumbers(numbers: List<Int>) {
    numbers.forEach {
        if (it < 0) return@forEach  // ✅ 仅跳过当前元素
        println(it)
    }
}

// ❌ 大集合多次中间操作（产生多个临时集合）
val result = bigList.filter { it > 0 }.map { it * 2 }.filter { it < 100 }

// ✅ 使用 Sequence 惰性求值
val result = bigList.asSequence().filter { it > 0 }.map { it * 2 }.filter { it < 100 }.toList()
```

---

## 七、类型系统与空安全

### 核心原则
- 使用 `T?` 可空类型、`?.` 安全调用、`?:` Elvis 运算符
- **禁止** `!!` 双感叹号断言（除非有充分理由并加注释）
- Java 互操作时立即声明明确类型，防止平台类型泄漏

### 关键规则
1. 使用安全转型 `as?` 代替强制转型 `as`（避免 `ClassCastException`）
2. 在 Java 互操作边界立即标注类型，消除平台类型（`T!`）
3. 为 Java 代码添加 `@Nullable` / `@NonNull` 注解
4. 公共 API 函数必须显式声明返回类型（避免暴露实现类型）
5. 复杂类型声明用 `typealias` 简化
6. `throw` 仅用于合同违反（`require`/`check`）和不可恢复错误

### 禁止事项
- ❌ 使用 `!!` 强制解包 nullable（应用 `?.`、`?:` 或 `let`）
- ❌ Java 互操作时平台类型泄漏到内部 API
- ❌ 公共 API 隐式推断返回类型（可能意外暴露实现类型）

### 反例 → 正例
```kotlin
// ❌ 连续 !! 导致潜在 NPE
val city: String = person!!.address!!.city!!

// ✅ 安全调用链 + Elvis
val city: String = person?.address?.city ?: "Unknown"

// ❌ 平台类型泄漏
val user = javaService.getUser()  // String! 类型泄漏
val name = user.name               // 仍是平台类型

// ✅ 立即声明明确类型
val user: User = javaService.getUser()  // 显式消除平台类型

// ❌ 隐式返回实现类型
fun createPhone() = DefaultPhone  // 暴露了实现类

// ✅ 显式返回接口类型
fun createPhone(): Phone = DefaultPhone
```

---

## 八、Data Class

### 核心原则
- 仅在纯数据存储时使用 data class
- data class 的所有属性应为 `val`（不可变）

### 关键规则
1. data class 不应有业务方法（只允许对象转换函数）
2. data class 不支持继承（不能用 `open`、`sealed`、`abstract`、`inner`）
3. data class 没有无参构造函数（影响 Jackson 等序列化框架，需 `@JsonCreator` 或插件）
4. `copy()` 是浅拷贝，内含可变对象仍共享引用
5. 解构限制在 3 个属性以内，禁止在公共 API 或 lambda 参数中使用解构

### 禁止事项
- ❌ data class 中使用 `var` 属性（破坏 Set/Map 等集合的行为）
- ❌ data class 中包含函数类型属性（影响 `equals`/`hashCode`）
- ❌ data class 中包含可变集合属性
- ❌ data class 中添加业务逻辑方法
- ❌ 超过 3 个属性的解构（可读性差）

### 反例 → 正例
```kotlin
// ❌ var 属性破坏 Set/Map 行为
data class User(val id: String, var lastLoginTime: Long)

// ✅ 全部 val
data class User(val id: String, val lastLoginTime: Long)

// ❌ 包含函数类型和可变集合
data class UserEvent(
    val onLogin: (String) -> Unit,         // 函数类型
    val permissions: MutableList<String>   // 可变集合
)

// ✅ 使用不可变值类型
data class UserInfo(
    val userId: String,
    val permissions: List<String>
)
```

---

## 九、泛型

### 核心原则
- 理解类型擦除：`List<Int>` 和 `List<String>` 在运行时是同一类型
- 合理使用型变声明：`out`（协变/生产者）、`in`（逆变/消费者）

### 关键规则
1. 同一 JVM 平台泛型重载会产生声明冲突，用 `@JvmName` 区分
2. 泛型中基本类型会被装箱，性能敏感时考虑专用实现
3. `inline` 函数配合 `reified` 类型参数，可在运行时获取泛型类型信息
4. 生产者用 `out`（只读），消费者用 `in`（只写）

### 禁止事项
- ❌ 同一文件中仅泛型参数不同的重载函数（会产生平台声明冲突）

### 反例 → 正例
```kotlin
// ❌ 泛型重载冲突
fun foo(list: List<Int>) { }
fun foo(list: List<String>) { }  // 编译器报错：平台声明冲突

// ✅ 用 @JvmName 区分
@JvmName("fooInt")
fun foo(list: List<Int>) { }
@JvmName("fooString")
fun foo(list: List<String>) { }
```

---

## 十、元编程（反射、注解处理）

### 关键规则
1. 禁止用 `Foo::class` 作为锁对象（不同类加载器下实例不唯一），应用 `Foo::class.java` 或 `this`
2. 反射（尤其是 Kotlin 反射）有高开销（需反序列化 `@Metadata`），避免在热点代码中使用
3. 序列化、DI、路由等场景优先用代码生成，而非运行时反射
4. 注解处理优先使用 KSP 而非 KAPT（KSP 更快，支持多平台，理解 Kotlin 语法）

### 禁止事项
- ❌ 用 `synchronized(Worker::class)` 加锁（类引用实例可能不唯一）
- ❌ 在性能敏感路径使用 Kotlin 反射（`Class.forName(name).kotlin`）
- ❌ 新代码使用 KAPT（已弃用趋势，推荐 KSP）

---

## 十一、ABI 兼容性（库/SDK 开发必看）

### 核心原则
- 公共 API 函数必须显式声明返回类型
- 不得修改已发布公共函数的签名

### 关键规则
1. 永远不要修改公共函数参数类型、顺序、数量
2. 永远不要为已发布函数新增默认参数
3. 不要在单参数和 vararg 之间切换
4. 不要修改类型继承关系
5. `inline` 函数的函数体修改不会传播到已编译的调用方（旧 ABI 保持旧行为）
6. 用方法重载替代修改现有函数签名
7. 使用 `@OptIn` / `@RequiresOptIn` 标记实验性 API

### 禁止事项
- ❌ 无显式返回类型的公共函数（类型推断结果可能随版本变化）
- ❌ 修改公共 API 参数类型/顺序
- ❌ 为发布版本的 `inline` 函数修改函数体

---

## 十二、Jetpack Compose（如适用）

### 核心原则
- Composable 函数只应描述 UI，不应在组合过程中修改状态
- 所有 State 读写在主线程执行

### 关键规则
1. 状态变更应在事件回调或 Effect Handler 中执行，不在 Composable 函数体内
2. 用 `derivedStateOf` 派生状态，确保仅计算结果变化时触发重组
3. 用 `rememberUpdatedState` 在长期副作用中安全访问最新 lambda
4. 状态使用 `by remember` 管理，对象缓存用 `val = remember`，避免 `var = remember`
5. I/O 等命令式操作封装在生命周期感知的 Effect Handlers（`LaunchedEffect`、`SideEffect`）中
6. 禁止长生命周期对象持有组合期间创建的 Composable Lambda（内存泄漏）
7. 频繁变化的值用 `compositionLocalOf`，极少变化的值用 `staticCompositionLocalOf`

### 禁止事项
- ❌ 在 Composable 函数体内直接修改 State（向后写入，导致无限重组）
- ❌ 嵌套 Composable 在组合过程中修改祖先读取的 State
- ❌ 长生命周期对象持有 Composable Lambda 引用

### 反例 → 正例
```kotlin
// ❌ 在组合过程中修改 State，引发无限重组
@Composable
fun BadCounter() {
    var count by remember { mutableStateOf(0) }
    Text(text = "Count: $count")
    count++  // ❌ 向后写入！
}

// ✅ 状态修改在事件回调中
@Composable
fun GoodCounter() {
    var count by remember { mutableStateOf(0) }
    Column {
        Text(text = "Count: $count")
        Button(onClick = { count++ }) { Text("Increment") }  // ✅
    }
}
```

---

## 审查清单速查

| 分类 | 检查项 |
|------|--------|
| **变量** | `val` 优先于 `var`；`lateinit` 无 `isInitialized`；作用域最小化；API 返回只读集合 |
| **函数** | `require`/`check` 检验合同；同类型参数使用具名参数；监听器持有引用 |
| **类** | 属性 getter 无 IO/异常；`sealed class` 替代带数据的 enum |
| **协程** | 无 `GlobalScope`；不吞 `CancellationException`；`Mutex` 替代 `synchronized`；CPU 循环有 `ensureActive` |
| **异常** | 不用异常做控制流；用 `OrNull` 系列函数；返回 `Result<T>` 而非抛异常 |
| **集合** | API 返回 `List` 而非 `MutableList`；`forEach` 无无标签 `return`；大数据用 `Sequence` |
| **空安全** | 无 `!!`；消除平台类型泄漏；公共函数显式返回类型 |
| **Data Class** | 全部 `val`；无函数类型属性；无可变集合属性；无业务方法 |
| **Compose** | 无向后写入 State；IO 在 Effect Handler 中；使用 `derivedStateOf` |
| **ABI** | 公共函数显式返回类型；不修改已发布签名；不改 inline 函数体 |
