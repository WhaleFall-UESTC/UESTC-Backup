# Kotlin 安全规范精要

> 基于 Kotlin 安全编码实践整理，参考 Java 安全规范

---

## 一、输入验证与注入防护

### 1.1 注入防护
- **【必须】SQL 注入防护**：使用预编译语句（PreparedStatement）和参数绑定；使用 ORM 框架时用参数占位符（Exposed、Room 等）
- **【必须】命令注入防护**：禁止将外部输入拼接到 `Runtime.exec()` 或 `ProcessBuilder`；不要创建 shell 进程执行用户输入
- **【必须】路径穿越防护**：使用 `File.canonicalPath` 验证路径，检查 `..`，禁止路径完全由外部输入控制
- **【必须】XXE 防护**：XML 解析器禁用 DTD 和外部实体
- **【必须】正则注入防护**：净化传递给 `Regex` 的非受信数据，注意正则 DOS（ReDoS）风险

### 1.2 数据验证
- **【必须】类型验证**：使用 `toIntOrNull()`、`toBigDecimalOrNull()` 等安全转换，不依赖异常捕获做验证
- **【必须】白名单过滤**：表名/列名等无法参数化的场景必须用白名单
- **【必须】输入长度限制**：验证外部输入的长度，防止缓冲区溢出或资源耗尽
- **【建议】使用 `require`/`check`**：在函数入口用 `require` 验证参数，用 `check` 验证状态

### 1.3 特殊场景
- **【必须】日志注入防护**：记录日志前净化用户输入，过滤换行符
- **【必须】Zip 炸弹防护**：解压前检查 `ZipEntry.size` 限制解压大小
- **【必须】序列化安全**：反序列化 JSON/二进制时，禁止使用 `enableDefaultTyping`（Jackson），避免任意类型实例化

---

## 二、Kotlin 特有安全风险

### 2.1 空安全滥用
- **【必须】禁止使用 `!!` 强制解包**：在生产代码中 `!!` 相当于显式放弃空安全，应使用 `?.`、`?:` 或早期 `return`
- **【必须】平台类型（`T!`）处理**：Java 互操作时立即声明明确类型，消除平台类型；为 Java 代码添加 `@Nullable`/`@NonNull` 注解
- **【建议】避免对不受信数据使用 Elvis 返回默认值**：`?:` 默认值不等于安全，需验证数据合法性

```kotlin
// ❌ 强制解包可能 crash
val city = user!!.address!!.city!!

// ✅ 安全处理
val city = user?.address?.city ?: throw IllegalArgumentException("City required")
```

### 2.2 协程安全
- **【必须】不在协程中使用 `synchronized`**：应使用 `kotlinx.coroutines.sync.Mutex`，避免线程阻塞引发死锁或饥饿
- **【必须】避免 `GlobalScope`**：可能导致敏感操作在用户登出或 Activity 销毁后仍在后台运行（生命周期泄漏）
- **【必须】正确传播取消**：不要吞掉 `CancellationException`，防止取消逻辑失效导致资源泄漏
- **【建议】结构化并发保证资源释放**：使用 `suspendCancellableCoroutine` 的 `invokeOnCancellation` 释放底层资源

### 2.3 反射与元编程
- **【必须】禁止动态加载不受信类**：`Class.forName(userInput)` 可能导致任意代码执行
- **【必须】序列化/反序列化时限制类型**：禁止根据外部输入决定反序列化目标类型（多态反序列化漏洞）
- **【必须】`synchronized` 锁对象安全**：禁止使用 `Foo::class` 作为锁（实例不唯一），使用 `object` 或专用锁对象

### 2.4 扩展函数安全
- **【注意】扩展函数可见性**：公开的扩展函数对所有使用者可见，避免将内部逻辑通过扩展函数暴露
- **【注意】operator 重载语义**：运算符重载应与运算符语义一致，防止调用方产生误判（如 `+` 不应有副作用）

---

## 三、文件操作安全

### 3.1 路径安全
- **【必须】禁止全路径可控**：文件路径不能完全由客户端传入
- **【必须】防止路径穿越**：使用 `File.canonicalPath.startsWith(baseDir)` 验证
- **【建议】随机化文件名**：存储用户上传文件时用 UUID 随机化文件名

### 3.2 资源管理
- **【必须】使用 `use` 自动关闭资源**：`InputStream`、`OutputStream`、`Reader`、`Writer` 等均应用 `.use {}` 块
- **【必须】临时文件清理**：使用 `File.createTempFile` 后，程序结束前删除临时文件
- **【必须】权限控制**：创建文件时设置最小必要权限

```kotlin
// ❌ 资源可能泄漏
val stream = File(path).inputStream()
val content = stream.readBytes()
stream.close()  // 异常时跳过

// ✅ use 自动关闭
val content = File(path).inputStream().use { it.readBytes() }
```

---

## 四、网络与传输安全

### 4.1 SSRF 防护
- **【必须】URL 白名单**：访问外部 URL 前验证域名白名单，不信任用户提供的 URL
- **【必须】内网 IP 过滤**：DNS 解析后过滤内网 IP 段（10.x、172.16-31.x、192.168.x）
- **【必须】禁用跳转**：HTTP 客户端禁用自动重定向（如 OkHttp `followRedirects(false)`）

### 4.2 SSL/TLS 安全
- **【必须】证书验证**：自定义 `X509TrustManager` 必须实现完整的 `checkServerTrusted`
- **【必须】域名验证**：自定义 `HostnameVerifier` 必须实现 `verify` 校验，禁用 `ALLOW_ALL_HOSTNAME_VERIFIER`
- **【建议】敏感数据使用 HTTPS**：禁止通过明文 HTTP 传输凭证、个人信息等敏感数据

### 4.3 Android WebView 安全（如适用）
- **【必须】API 18 以下禁用 `addJavaScriptInterface`**
- **【建议】禁用文件协议**：`setAllowFileAccess(false)`
- **【必须】SSL 错误处理**：`onReceivedSslError` 不能直接调用 `proceed()`，应终止加载

---

## 五、密码学与密钥管理

### 5.1 避免弱加密
- **【必须】禁用弱算法**：禁止使用 MD5、SHA-1 作为密码散列，禁止使用 DES/3DES
- **【必须】密码存储**：使用 BCrypt、Argon2 等自适应散列算法，不使用普通哈希（即使加盐）
- **【必须】随机数安全**：密码学场景使用 `SecureRandom`，不使用 `Random` 或 `Math.random()`

### 5.2 密钥管理
- **【必须】禁止硬编码密钥**：密钥、密码、Token 不得出现在源代码或版本控制中
- **【必须】密钥存储安全**：Android 使用 `AndroidKeyStore`，服务端使用密钥管理服务（KMS）
- **【必须】密钥轮换**：设计支持密钥轮换的机制

```kotlin
// ❌ 硬编码密钥
val secretKey = "my_hardcoded_secret_key"

// ✅ 从安全存储读取
val secretKey = keyStore.getKey("myKeyAlias", null)
```

---

## 六、会话与权限

### 6.1 会话管理
- **【必须】凭证传输安全**：非一次性凭证禁止通过 URL 传输（会出现在日志、Referer 等）
- **【必须】Token 存储安全**：Android 使用 `EncryptedSharedPreferences` 或 `Keystore` 存储 Token
- **【必须】登出清理**：用户登出时清除本地缓存的凭证和敏感数据

### 6.2 权限检查
- **【必须】服务端鉴权**：不依赖客户端传递的用户 ID 做权限判断，需服务端通过 Token 解析
- **【必须】最小权限原则**：Android 权限申请仅申请实际需要的权限

---

## 七、数据保护与隐私

### 7.1 敏感数据处理
- **【必须】日志脱敏**：禁止在日志中输出密码、Token、身份证号、银行卡号等敏感信息
- **【必须】异常信息安全**：生产环境禁止将完整异常栈返回给客户端
- **【必须】内存中的敏感数据**：使用完毕后及时清零（如密码字符数组）
- **【建议】`data class` 重写 `toString`**：避免自动生成的 `toString` 输出敏感字段

```kotlin
// ❌ data class 的 toString 会泄露密码
data class LoginRequest(val username: String, val password: String)

// ✅ 重写 toString 隐藏敏感字段
data class LoginRequest(val username: String, val password: String) {
    override fun toString() = "LoginRequest(username=$username, password=***)"
}
```

### 7.2 Android 数据存储
- **【必须】不在 SharedPreferences 中明文存储敏感数据**：使用 `EncryptedSharedPreferences`
- **【必须】数据库加密**：存储敏感数据的 SQLite 数据库使用 SQLCipher 等加密

---

## 八、Android 特定安全（如适用）

### 8.1 组件安全
- **【必须】最小化组件导出**：非必要不导出 Activity、Service、BroadcastReceiver、ContentProvider（`android:exported="false"`）
- **【必须】隐式 Intent 过滤**：敏感操作不使用隐式 Intent，避免被恶意应用拦截
- **【必须】Deep Link 验证**：处理 Deep Link 时验证来源和参数合法性

### 8.2 WebView 安全
- **【必须】禁止 `addJavaScriptInterface`（API < 17）**
- **【必须】本地 HTML 加载限制**：加载本地 HTML 时禁止访问文件系统（`setAllowFileAccess(false)`）
- **【建议】JS Bridge 鉴权**：通过 JavaScript Bridge 暴露 Native 能力时，添加来源验证

---

## 安全审查检查清单

| 分类 | 检查项 |
|------|--------|
| **注入防护** | SQL 预编译；无命令注入；路径白名单验证 |
| **空安全** | 无 `!!` 强制解包；平台类型立即标注 |
| **协程安全** | 无 `GlobalScope`；不吞 `CancellationException`；资源在 `invokeOnCancellation` 中释放 |
| **资源管理** | 使用 `.use {}` 关闭 IO 资源；无资源泄漏 |
| **密码学** | 无弱加密算法；无硬编码密钥；使用 `SecureRandom` |
| **数据保护** | 日志无敏感信息；`data class` 重写 `toString` 隐藏敏感字段 |
| **网络安全** | HTTPS；证书验证；URL 白名单 |
| **序列化** | 禁止任意类型反序列化；限制反序列化目标类型 |
| **Android** | 组件最小导出；WebView 安全配置；本地存储加密 |
