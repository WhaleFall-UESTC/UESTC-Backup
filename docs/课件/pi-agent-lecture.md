---
source: ../../课件/pi-agent-lecture.pdf
pages: 39
converted: 2026-11-24
---

# pi-agent 讲座

<!-- page 1/39 -->

π
              软件开发综合实验  · A I 专题
  拆解一个极简编码 Agent

         从安装配置到日常使用，再到原理
         看看一个 Agent 到底是怎么工作的

李忻洋 · 电子科技大学    范本：pi（MIT 开源）   终端里的结对编程伙伴     39 页 · 循序渐进





          参考：pi 源码与官方文档  · pi.dev  · 社区分析笔记（dg-ai-notes）

---

<!-- page 2/39 -->

开场
现在，大部分代码是 AI 帮你写的

   API 级别的细节——让 AI 去查，比你手敲快

 但你得看得懂它写的东西、出问题知道往哪查、它跑偏时拉得回来

 这种能力不来自背 API，来自对机制的理解

 今天以 pi 为标本：先装会用，再拆开看它怎么运转


 学框架的目的变了：以前是为了自己写，现在更多是为了驾驭 AI 写的。懂机制的人指挥 AI，一句话就能把跑偏的 Agent 拉回来；不懂的人只能全盘
 照收。

---

<!-- page 3/39 -->

开场
今天走三步


 阶段       内容                                     学完你会……

 ① 安装配置   环境、装包、接模型、写配置                                       10 分钟让它开口干活

 ② 日常使用   交互模式、命令、快捷键、会话管理                    把终端变成你的结对程序员

 ③ 原理         Agent Loop、工具、模型、消息、上下文                 看懂 90 词提示词背后的发动机

 三个阶段互相独立。只跟到第二部分，你已经是 pi 的用户了；跟完第三部分，你就是能给别人讲 Agent 的人。

---

<!-- page 4/39 -->

认识 P I
它不是一辆整车，而是一盒积木

 极简终端编码 Agent 外壳（harness）：住在终端里，没有 GUI、没有 IDE                                        作为工具          作为教材
 插件                                         开箱即用的结对编程伙伴，    核心够小，源码读得完、讲
                                                                                                    pi 一回车就开始干活      得清——所以选它当范本
 作者 Mario Zechner（libGDX 游戏引擎作者），TypeScript 编写，MIT 协
 议
                                        作为 SDK         一句话
 内置工具只有 4 个：read / write / edit / bash                 四层包各可独立使用，拿来    「世上最可驾驭的外壳」：
                                             嵌进你自己的应用        每个动作你都有否决权和改
  刻意不做：MCP、子 Agent、计划模式、权限弹窗——但每个都能自己搭                                                             造权

   Cursor 像一辆造好的整车，坐进去就能开。pi 给你的是发动机、底盘、转向柱
 ——它把「做减法」的权力留给你：不想要的功能可以拆，想要的功能可以装。

---

<!-- page 5/39 -->

认识 P I
几个数字，看懂什么叫「极简」

 4         ~90 词     30+        4         64k+
 核心工具：read · write ·    静态系统提示词（运行时拼   支持的模型供应商       运行模式：交互 /             GitHub Stars——社区用脚
  edit · bash          接后 200–400 词）             Anthropic / OpenAI /          print·JSON / RPC / SDK    投票
 另有 grep / find / ls 三个    对比某些商业 Agent 的数      Google / DeepSeek /
 辅助             万字                      Groq / Ollama……


 提示词为什么这么短？因为「该说什么」应该由你决定，不该被框架预判——你随时可以用自己的 AGENTS.md 把它盖掉。

---

<!-- page 6/39 -->

认识 P I
四个核心包，各司其职

  pi-coding-agent：完整 CLI 产品 + SDK——系统



                                                                                                                                                                                     LAYER 4 · 产品+SDK 提示词、内置工具、会话、扩展
                                                                                                   pi-coding-agent
                                                                                                                                                                                                                                                                                                                                createAgentSession()
                                                                                  系统提示词 · 内置工具 · 会话管理 · 扩展 · CLI  pi-agent-core：引擎——Agent Loop、工具系


                                                                                                                                                      依赖
 统、事件流、压缩
                                                                                                                                                                                                                                                          PARALLEL · UI
                                                                                                                                                                                       LAYER 2 · 引擎
  pi-ai：多供应商 LLM 统一 API + 流式 + Token 追                       pi-agent-core                                                               pi-tui差分渲染 · 组件系统 · Markdown
                                                                                                                                                                                                                                                   ~12000行·不依赖任何pi-xxx
 踪                                                                                                                                       AgentLoop · 工具系统 · 事件流 · Session · Compaction
                                                                                                                                                      依赖
  pi-tui：终端 UI 库（差分渲染），与 Agent 体系                                              LAYER 1 · 模型

                                                                                                                    pi-ai 正交解耦
                                                                                  统一 API · 上下文交接 · 流式 · Token 追踪 · 30+ 提供商
 依赖方向永远向上——底层对上层一无所知


                                                                                                                                                FOUR RUN MODES


                                                                                     交互模式                            print/JSON                       RPC 模式                       SDK 模式

                                                                                                                                                                                                                         pi                                            pi -p "..."                                    stdin/stdout JSON                 嵌入自己的应用



                                                                                      依赖方向永远向上——底层对上层一无所知。这是分层架构的核心承诺。

                                                              图：pi 四层架构（图源：社区笔记 pi_source_dive）

---

<!-- page 7/39 -->

01
第一部分
安装与配置

目标：10 分钟，让它在你的终端里开口说话


                       ① 安装配置    ② 日常使用    ③ 原理

---

<!-- page 8/39 -->

安装配置
开工前，检查两样东西

① Node.js ≥ 22                                               类比：pi 是「期末考试」，Node.js 是「入学资格」。node --
这是 SDK 的硬约束（package.json 里的 engines），版本太低直接         version 显示 v18？先回去补入学手续，别急着坐考场。
跑不起来。
                                   顺手记下                  Windows 注意
  node --version                               配置目录默认在 ~/.pi/agent/ ，      Alt+Enter 默认是全屏，需要重映
  # 期望看到 v22.19.x 或更高                         后面所有配置都住这里        射才能用 pi 的消息排队快捷键
                                                          （第二部分会讲）
② 一个你顺手的终端
Windows 推荐 Git Bash 或 Windows Terminal（快捷键略有差异，
官方文档有专门说明）。

---

<!-- page 9/39 -->

安装配置
一条命令装好


  npm install -g --ignore-scripts \                       包名很长          升级
    @earendil-works/pi-coding-agent                                                     @earendil-works/pi-             pi update --self
                                                                                           coding-agent          一键自升级，装过的扩展包
 为什么要 --ignore-scripts？关掉依赖的生命周期脚本——pi 正常安装根     好在命令就一个字母：pi     也能一起升
 本不需要跑脚本，这是供应链安全的习惯                     ——回车即开

  装完验证：pi --version
                                        课堂提醒
                                             全局安装需要写全局目录权 备选方案（不想走 npm 的同学）：
                                             限；嫌麻烦就用 nvm 管理的
     curl -fsSL https://pi.dev/install.sh | sh                              Node 来装

---

<!-- page 10/39 -->

安装配置
第一次启动：给它办个「车牌」

路线 A：API Key（按量付费）                                          信息安全第一课：API key 放环境变量或 pi 的凭据存储——别写进代
  export ANTHROPIC_API_KEY=sk-ant-...                  码、别提交进仓库。这条规则对人和 AI 都一样：AI 生成的代码里漏
  pi                                                                key 的事故可太多了。

路线 B：已有订阅（交互式登录）                                   本地党福音           自定义供应商
  pi                                                                          llama.cpp 路由：/login               ~/.pi/agent/models.json 加几
  /login   # 选择供应商                                                       llama.cpp ，模型全跑在自己机器   行就能接
                                        上                               OpenAI/Anthropic/Google 兼容
订阅支持：Claude Pro/Max、ChatGPT Plus/Pro（Codex）、GitHub                          接口
Copilot；API Key 支持 30+ 家：OpenAI、Google、DeepSeek、Groq、
OpenRouter、Ollama（本地）……

---

<!-- page 11/39 -->

安装配置
选对模型，调好「思考深度」

    /model 或  Ctrl+L ：模型选择器；在列表里  Ctrl+S 存为启       pi --model openai/gpt-4o # 指定提供商/模型
 动默认                                                             pi --model sonnet:high    # 顺便拉满思考级别
                                                                       pi --thinking medium -p "总结这个仓库"
    /thinking ：思考级别 off / low / medium / high / xhigh /
  max，Shift+Tab 快速切换                                     思考级别像作业上的「赶工 / 细做」开关：改错别字用 low，重构核
                                     心模块用 high。它直接影响速度和花费——后面看底栏的费用时就  命令行列出全部可用模型：pi --list-models
                                     有体感了。

---

<!-- page 12/39 -->

安装配置
配置：一个全局，一个本地


 位置                          作用

  ~/.pi/agent/settings.json              全局设置（对所有项目生效）

  .pi/settings.json                 项目设置（覆盖全局）——进项目自动生效

  AGENTS.md  / CLAUDE.md               上下文文件：全局 + 父目录逐级向上 + 当前目录，按顺序拼接，等于给 Agent 的「班规」

  ~/.pi/agent/prompts|skills|extensions|themes/   四类定制资源目录（第二部分细讲）

  ~/.pi/agent/SYSTEM.md                    直接替换默认系统提示词；APPEND_SYSTEM.md 则是追加

  项目信任（Project Trust）：第一次进一个带 .pi 资源的项目，pi 会先问「信不信这个项目」——信了才加载它的扩展和设置（/trust 保存决
  定）。AI 时代，「谁给你的指令可以执行」本身就是安全边界。

---

<!-- page 13/39 -->

02
第二部分
日常使用

装好了？好戏开始了。


                       ① 安装配置    ② 日常使用    ③ 原理

---

<!-- page 14/39 -->

日常使用
打开以后长什么样？

 启动头：快捷键提示 + 已加载的 AGENTS.md、技
 能、扩展

 消息区：你的消息、模型回复、工具调用与结果、
 通知

 编辑器：打字的地方；边框颜色 = 思考级别

 底栏：目录 · 会话名 · token 用量 · 花费 · 上下文占
 用 · 当前模型





                                                                   图：pi 交互模式（官方文档截图）



 没有 GUI、没有 IDE 插件，一切输出进终端回滚缓冲区。丑吗？不丑——是轻。它不跟你抢屏幕，只在你需要时说话。

---

<!-- page 15/39 -->

日常使用
编辑器里有几个暗门


 按键 / 语法        作用

  @           模糊搜索项目文件，直接把文件「喂」给模型

   Tab          路径补全

   Shift+Enter        多行输入（Windows Terminal 用 Ctrl+Enter）

   Ctrl+G           甩给外部编辑器（$EDITOR / Notepad / nano），写完无缝回来

  !命令         执行命令，把输出发给模型

  !!命令         只执行，不发给模型

   Ctrl+V  / 拖图     粘贴图片（截图、报错图都可以）

 你敲 !ls ，模型立刻「看见」输出——相当于拍着它的肩膀说「看这个」。调试时比复制粘贴优雅多了。

---

<!-- page 16/39 -->

日常使用
/ 命令：Agent 的中控台


 命令             干什么                      命令             干什么

  /login          管理供应商凭据                                 /export   · /import     导出 HTML/JSONL、导入恢复

  /model              切换模型（Ctrl+S 存默认）                       /share           上传成可分享的 HTML 链接

  /resume   · /new      恢复旧会话 / 开新会话                           /settings         主题、传输方式等偏好

  /tree           跳到会话任意历史点继续写                       /reload          热重载扩展、技能、提示词、主题

  /fork   · /clone      从旧消息分叉 / 克隆当前分支                     /hotkeys         查看所有快捷键

  /compact         手动压缩上下文                                 /quit           退出





 不用背全——敲 / 就有补全。而且扩展能注册自己的斜杠命令：你以后看到的某些 /xxx，可能根本不是官方自带的。

---

<!-- page 17/39 -->

日常使用
手不离键盘

 Esc 中止当前运行（双击开 /tree）    Ctrl+L 模型选择器    Ctrl+P 快速轮换模型    Shift+Tab 切换思考级别
 Ctrl+O 折叠 / 展开工具输出    Ctrl+T 折叠 / 展开思考块    Ctrl+X 复制上一条模型回复    Ctrl+C 清空编辑器（双击退出）

   Esc 是「超时铃」：发现模型跑偏，敲一下，正在执行的那批工具立刻停下，已输入的内容退回编辑器。Agent 失控感的最快解法，就这一键。

---

<!-- page 18/39 -->

日常使用
它正干着活，你想改道？

    Enter ：排队一条 steering（转向）消息——当前这轮工具跑

                                                                                                                                                                           STEERING · 紧急插队                                                FOLLOWUP · 排队等叫号

                                                                                                                        生活类比：开会时有人敲门递纸条                                   生活类比：开完会翻信箱 完，先读它再干活，相当于中途插话



                                                                                                              循环时间线                                           循环时间线
                                                                                                                                                                                                                                             Turn 1               Turn 2               Turn 3                                                     Turn 1               Turn 2             Turn 3 (停)    Alt+Enter ：follow-up（追加）——等它全部干完再处理                                                  每圈检查
                                                                                                                                                                                                              内层循环结束



                                                                                                                                                                                                           检查 followUp
                                                                                                                                                                                                             队列    Esc ：中止；Alt+Up ：把排队消息取回编辑器改一改                                                                                                  steering 入队



                                                                                                                           → 在 Turn 3 开头注入                                                       新 Turn（续命）
 类比开卷考试：学生做到一半你喊「先别写，回头看看例题」——这                              （当前 Turn 正常完成后注入，不打断执行）                                 → 在同一个 Trace 内继续跑


                                                                                                                                                                                 CHARACTERISTICS                                                             CHARACTERISTICS 是 steering；「做完这套再来一套」——这是 follow-up。
                                                                                                                时机：每圈内层循环开头 + 结尾                                  时机：内层循环全部结束后

                                                                                                                来源：用户实时输入的新指令                                     来源：系统追加（如"顺便跑测试"）

                                                                                                                影响：会驱动循环继续（不等当前完成）                              影响：触发外层循环，内层循环重开


                                                                                                                                                                                                                                getSteeringMessages()                                                                           getFollowUpMessages()

                                                                                                                   用户让Agent修bug，中途想起"也检查测试文件"                                                    Agent完成任务后，系统追加"顺便跑个测试"


                                                                                        图：steering 与 follow-up 的投递时机

---

<!-- page 19/39 -->

日常使用
会话：对话被存成了一棵「树」

  每条消息一个节点（id + parentId），JSONL 文
 件，自动存在 ~/.pi/agent/sessions/

    /tree ：跳到任意历史点原地继续——后悔了？
 回退，重写

    /fork ：从某条旧消息切出新分支；/clone ：
 克隆当前分支

  命令行：pi -c 续最近会话，pi -r 浏览历史





                                                                              图：/tree 会话树视图（官方文档截图）



 可以把它理解成对话界的 Git：每个后悔点都能回滚，任何历史点都能开新时间线。原理部分会看到，实现起来其实非常便宜。

---

<!-- page 20/39 -->

日常使用
聊太久了？自动「课堂小结」

  模型上下文窗口有硬顶（200K / 128K……），会话是无限长的——早晚撞墙

 自动压缩默认开启：撞墙时压缩后重试；快到墙时主动压缩

    /compact 或 /compact 保留测试细节：手动压缩，还能附指令

 压缩是有损的，但完整历史仍在 JSONL 里，/tree 随时翻回去


 像学生快忘光时做的课堂小结：大意还在，细节丢了。pi 的做法是「做小结，但全文录像都留着」——所以敢自动做。

---

<!-- page 21/39 -->

日常使用
pi 不止会聊天

                                    SDK：嵌进自己的应用  pi -p "总结这个代码库"
  cat README.md | pi -p "这段文本讲什么？"                                                                       import { createAgentSession,
  pi --mode json -p "..."   # 事件流 JSONL                                                                         ModelRuntime, SessionManager }
  pi --mode rpc             # stdin/stdout JSON                                                                         from "@earendil-works/pi-coding-agent";

  -p：问完就退，适合脚本和 CI                                                                       const rt = await ModelRuntime.create();
   json / rpc：让其他程序「驾驶」pi                                  const { session } = await createAgentSession({
                                                                         sessionManager: SessionManager.inMemory(),
  还能裁剪工具：--tools read,grep 只读模式跑审查                                                                         modelRuntime: rt,
                                                                       });
                                                                       await session.prompt("当前目录有哪些文件？");


                                                 聊天是演示；json、rpc、SDK 才是把 pi 变成组件的入口——学完原
                                     理，这十几行代码你每行都看得懂。

---

<!-- page 22/39 -->

日常使用
pi 没有的功能，全都能自己搭


 技能 Skills         提示词模板        扩展 Extensions      主题 Themes               Pi 包
 「指令 + 工具」的能力     可复用的 markdown 提示       TypeScript 模块：自定义       dark / light + 自定义，改    把上面四样打包，pi
 包，按需加载——不调用就    词，/review 一键展开，   工具、命令、事件钩子、整   文件立即生效                  install 走 npm / git 分
 不占一个 token        支持 {{变量}}         套 UI，热重载                                 享（pi list / pi config 管
  （/skill:name 触发）                                                   理）


 子 Agent、计划模式、MCP、权限弹窗……商业 Agent「焊死在产品里」的东西，pi 全拆成了可选模块，官方仓库自带 50+ 扩展示例。安全提醒：
 第三方扩展拥有完整系统权限，装之前先看源码。

---

<!-- page 23/39 -->

日常使用
等模型的时候，能玩 Doom

                                  扩展能碰：工具、斜杠命令、快捷键、事件钩子、整个 TUI 组件
                                  树

                                  能整出：子 Agent、权限门禁、沙箱、MCP 接入、自定义编辑
                                                    器、git 自动提交……

                                  最妙的玩法：让编码 Agent 自己改自己的能力——「给 pi 加个
                                                                   xxx 功能」，它写完，/reload，生效

                                                           「Pi 不藏私，把内脏都暴露给你」——这句话在原理部分会变成具体
                                     代码：事件 + 工具 + 组件，三样东西组合出上面所有功能。





       图：等待模型响应时，编辑器里跑着 Doom（官方示例）

---

<!-- page 24/39 -->

03
第三部分
原理

90 词提示词 + 4 个工具，发动机室里到底装了什么？


                       ① 安装配置    ② 日常使用    ③ 原理

---

<!-- page 25/39 -->

原理
先退一步：用大模型有三种方式


 维度       ① 直接调用          ② Workflow                 ③ Agent Loop

 决策者     你              你的代码                  模型

 调用次数        1 次                N 次（代码写死）              不确定（模型决定）

 核心工作    写提示词           设计流程                  定义工具 + 循环

 典型场景    翻译、摘要           文档流水线、RAG              编程助手、自动化任务

   pi 是第三种：你唯一要定义的停止规则是——模型某次输出里不再包含工具调用 = 活干完了。「下一步干什么」由模型说了算。

---

<!-- page 26/39 -->

原理  · A G E N T L O O P
循环解剖：Trace 与 Turn

   Trace = 一次完整运行：从你按回车到 agent_end，包含多个                                                                                          agent_start
   Turn
                                                                                                                                                                   TRACE
   Turn = 一次模型调用 + 该次调用触发的一整批工具                                一次完整运行Turn 1                                      toolUse      Turn 2                                      toolUse      Turn 3                                        stop

 内层循环每圈：处理待处理消息 → 调模型 → 检查 stopReason                                  turn_start    调用模型                         turn_start   再次调用模型                       turn_start    调用模型
 → 执行工具                                                                                                                                                                                                                        streamSimple(model, ctx)                    消息中带上工具结果                     看到工具结果，给出解释

                                                                                                             执行工具（一批）                 执行工具（一批）                  （无工具调用）

                                                                                                                                                                                                                                                                           read + grep + find                                                  edit                                                    toolCalls 为空   stopReason = toolUse ：把结果喂回去，进入下一圈；=


                                                                                                                                                                                                                    turn_end                                                                                                                                                                                                                                                                        turn_end                                                                                                                                                                                                                                                                                                                            turn_end    stop ：收工                                                                                                                                                                                                                                     stopReason = toolUse                                                                                                                                                                                                                                                                                              stopReason = toolUse                                                                                                                                                                                                                                                                                                                                                      stopReason = stop

                                                                                                                             → 把工具结果喂回去                                                    → 循环准备退出





                                                                                                                                                                                                                                             agent_end



                                                                                                                                                                                                                          LEGEND      焦点：模型/工具环节        事件边界      一个Turn=一次模型调用+其触发的所有工具执行

                                                      图：一个 Trace 里嵌套的 Turn





 一句话版：模型回答 → 说「我要看文件」→ 给它文件 → 再答 → …… → 说「好了」→ 停。整台发动机就是这几行 while 循环。

---

<!-- page 27/39 -->

原理  · A G E N T L O O P
循环本体 + 三层套壳

 最内核：模型调用 ↔ 工具执行的最小循环

                                                                                                                                                                                                                            + 钩子（产品功能）
  + steering：每圈检查队列，你插的话在这里插进对话（对应前                                                                            内核
                                                                                                                                                                                                                                        + followUp（任务追加）                                               最简 Loop：调模型 +
                                                                                                                                                                                执行工具 + stopReason 面的 Enter 排队）                                                                                                                                                            通用法则，所有Agent都需要


                                                                                                                                                                                                                                                    + steering（紧急插队）

                                                                                                                                                                                                                                                                                  + steering  + follow-up：整段任务做完后，把你追加的话作为新一轮                                                                                        紧急消息插队，
                                                                                                                                                                                                                                                                                                                                    KERNEL                                             用户在工作中补充指令

                                                                                                                                                                                              交互式产品的真实需求
                                                                                               最简 Loop（~10 行）  + 钩子：产品功能层——压缩、会话落盘、事件广播都挂在这                                                                                     while (hasMoreToolCalls)
                                                                                                                                                     调模型 → 检查 stopReason                                                   + followUp
                                                                                                                                                      → 执行工具 → 再调模型                               任务追加续命，
                                                                                                                                                                                                                                                                                                                                      Agent 完成后顺势追加
                                                                                                                                     所有Agent都需要的内核                                同一个Trace内继续跑



                                                                                                                                                                                                                                                                                  + 钩子

                                                                                                                                                                                                                                                                                 prepareNextTurn（切模型）
                                                                                                                                                                                                                                                                                               shouldStopAfterTurn（安全阀）

                                                                                                                                                                                              外部扩展点，按需注入





                                                                                                                                                                              INSIGHT    剥掉所有叠加层，最简Loop仍能正常工作。这是判断"内核是否被污染"的试金石。

                                                                             图：Agent Loop 的洋葱结构





 注意设计品味：核心循环不知道 steering 的存在；每层只加一种能力，可以单独理解、单独测试。这是「小而正确」的循环怎么来的答案。

---

<!-- page 28/39 -->

原理  · 工具系统
Agent 的手脚是怎么被管住的

 一个工具 = 名片 + 执行能力：name / description / 参数



                                                                                                                                                                                                       LAYER  1 · PI-AI                                          LAYER  2 · PI-AGENT-CORE                                  LAYER  3 · PI-CODING-AGENT  Schema，加上一个真正干活的函数                                                  原子层                          分子层                          材料层


                                                                                                                                                                              Tool                     interface                        AgentTool          extends Tool                           ToolDefinitionstructurally compatible   description 写给模型看——它是模型决定「用不用这个工具」的
                                                                                                                                                                                                 LLM 需要知道的：                                                   Agent 需要知道的：                           产品层需要的：

                                                                                                                                                                                                                            name                             string                                name            ↑ 继承                ↑ AgentTool 全部字段 全部依据，写得好不好直接决定 Agent 聪不聪明
                                                                                                                                                                                                                            description                    string                                description        ↑ 继承
                                                                                                                                                                                                                                                                                                       extends                                                               extends         + promptSnippet    + 自动拼 prompt
                                                                                                                                                                                                                            parameters             TSchema                                parameters         ↑ 继承
                                                                                                                                                                                                                                                                                                                                                            + promptGuidelines     + 使用守则
 参数用 Schema（TypeBox）校验：模型传错类型，框架先拦                                                                                                                                                                                                                                                                                            + label            新增                                + renderCall()        + 自定义渲染
                                                                                                                   关注点：
 住，不丢进系统里制造事故                                                                                     "工具长什么样"                                                    ++ prepareArguments()execute()          新增新增                                ++ renderShellexecute(ctx)    + 注入 ExtContext+ 渲染模式

                                                                                                                                                                                                                                                                                            + executionMode       新增
                                                                                                                                                                                                                       CAN BE USED ALONE                                                             关注点：
                                                                                                                          可独立发布，无 Agent 依赖                       关注点：                                 "工具怎么显示和接入
                                                                                                                                                                                                                        扩展系统"  内置工具：read / write / edit / bash （+ grep / find / ls）                                                           "工具怎么执行"





                                                                                                                                                                                  DESIGN PRINCIPLE

                                                                                             "底层定义最小类型，上层通过继承和联合类型扩展——每一层只加自己该关心的字段，不改底层。"

                                                                                                              这保证了底层可以独立发布和复用。别人可以只引用 Tool 类型，不引入整个 Agent 框架。





                                                                                                                   类型扩展链条·同样的"消息"也走这条路：Message→AgentMessage→业务专用消息类型

                                                       图：工具类型的三层递进

---

<!-- page 29/39 -->

原理  · 工具系统
一次工具调用，走五步

    1 参数预处理：补默认值、修 LLM 的小毛病                                                                                                  LLM 输出 ToolCall

                                                                                                                                                                                                                                                                                                               { name, arguments }


                                                                                                                                                                                                                                                                                                                                       FAILURE PATH    2  Schema 校验：类型 / 必填 / 范围                                                                                                                                        每步都可中断

                                                                                                                                                                                                            prepareArguments
                                                                                                                                                                                                                     1  参数预处理 · 处理 LLM 参数怪癖                                    参数预处理失败
                                                                                                                                                         例：补默认值、类型修正    3 执行前检查：权限、路径保护（可扩展）



                                                                                                                                                                                                                     2    TypeBox Schema 校验                                         类型校验不通过    4 执行：跑工具函数                                                                                                                                                               validateToolArguments
                                                                                                                                                                          参数类型/必填/范围检查
    5 结果包装：截断超长输出，作为 toolResult 消息喂回模型                                                                            beforeToolCall
                                                                                                                                                                                                                     3  权限检查 · 可阻止执行                                         权限拦截 / block
                                                                                                                                           返回{block:true}→拒绝




                                                                                                                                                                                                                                                 tool.execute()
                                                                                                                                                                                                                     4  真正执行 · 通过 Operations 接口                                     执行抛出异常
                                                                                                                                                                                                                                                                                                                                                  fs.readFile/命令执行/...




                                                                                                                                                                                                                                                afterToolCall
                                                                                                                                                                                                                     5  后处理 · 日志、审计、修改结果                                     后处理修改结果

                                                                                                                                                         例：脱敏、记录执行时长




                                                                                                                                                                                                                                   ToolResultMessage

                                                                                                                                                                                                                                                                                                       { isError: false, content: [...] }


                                                                                                                                                                            → 同样产 ToolResultMessage（isError: true）



                                                                                                                                                                                                          DESIGN   所有失败最终都编码成isError:true的消息——循环不被异常打断。这就是"永不抛出"原则。

                                                       图：工具执行五步流水线



 模型传参传错，别急着骂模型「笨」——是防线没拦住。好的 Agent 框架把「模型会犯错」当默认前提，层层设卡。

---

<!-- page 30/39 -->

原理  · 工具系统
一批工具，能同时跑吗？

 同一个 Turn 里，模型可以一口气要 3 个工具（read + grep +                                             这批工具里有任何 sequential 工具？

                                                                                                                                                                                             "一票否决"策略

                                                                                                                                                                宁可多等，不可出错   find）
                                                                                                                                              否                                                   是



                                                                                                                                                                             PARALLEL · 三阶段设计                                             SEQUENTIAL · 瀑布式 全是只读 → 并行跑，省时间                                                                                          省时间但保证正确性                                         一个完全结束才开始下一个

  只要有一个会改世界（bash、write、edit）→ 整批串行，「一票                          TIME →                                                                                  TIME →

                                                                                                                          阶段 1 · 准备（顺序）

                                                                                                                                                                                  A 准备               B 准备               C 准备                              ToolCall A 否决」                                                                                                                                         验证+权限             验证+权限             验证+权限                准备 → 验证 → beforeHook → execute → afterHook → emit end
                                                                                                                                                                                                                 完整管道跑完

                                                                                                                          阶段 2 · 执行（并行）★
 宁可多等，不可出错：并行的前提是没有副作用                                                                                   A.execute()              B.execute()              C.execute()                           ToolCall B
                                                                                                                                                                               准备 → 验证 → beforeHook → execute →                                                                                                                                                                                                                                                                                                                                                                                     afterHook → emit end
                                                                                                                                                                                                                                                                                                          Promise.all                                                                                                                                                                                                                                                                                                                                           Promise.all                                                                                                                                                                                                                                                                                                                                                                             Promise.all
                                                                                                                                                                                                                 完整管道跑完
                                                                                                                                               同时跑                                                                                                                                                               同时跑                                                                                                                                                                               同时跑


                                                                                                                          阶段 3 · 事件（有序）                                                                             ToolCall C
                                                                                                                                                                                                                             end 按完成顺序 · result 按调用顺序（A→B→C）                     准备 → 验证 → beforeHook → execute →完整管道跑完afterHook → emit end





                                                                                                                                                                                                                 WHY                                                                                  WHEN TO USE

                                                                                                                                 准备阶段必须顺序：B 被拦截了 C 就不该跑                                扩展工具显式声明 executionMode: "sequential"
                                                                                                                            结果消息保持顺序：模型依赖调用顺序                                                                         v0.80.2 内置工具均默认 parallel；edit 靠内层 mutation queue

                                                    图：并行 / 串行的判定与三阶段执行





 和实验室安全守则一个逻辑：「看」试剂瓶可以同时进行，「开火」必须排队。

---

<!-- page 31/39 -->

原理  · P I - A I
一家「翻译公司」

  30+ 供应商各有各的「方言」：消息格式、工具调用语法、思考
                                                                                                                                                                                                                                                                                                                                                    CLIENT

                                                                                                                                                                                         Agent Loop  级别……互不兼容                                                                                                                                                                                                                                   streamSimple(model, context, options)

   pi 内部只有一套统一消息格式，调用就一行：

                                                                                                                                                                                                                                                                                                                      LAYER 1 · 入口
    streamSimple(model, context, options)                                                                                  REGISTRY"通讯录"                             查表 resolveApiProvider(model.api)前台：stream() → 派给翻译员                                           SIMPLE思考级别翻译WRAPPER
                                                                                                                                                                                                                                             BUILTIN_APIS[]                                                                                                                                                + maxTokens 调整
 翻译公司的活：查「通讯录」→ 译成方言 → 发出 → 流式收回 →

                                                                                                                                                                                                                                                                                                                              LAYER 2 · 事件协议

                                                                                                                                    标准报告：12 种 AssistantMessageEvent 译回统一格式                                                                                                                                                                                                                                                              start · text_* · thinking_* · toolcall_* · done · error




                                                                                                                                                                                                          TRANSLATORS
                                                                                                                                                                                                        Anthropic 翻译员                     OpenAI 翻译员                       Google 翻译员                   Bedrock 翻译员  思考级别、maxTokens 这些差异，也在同一层被抹平                               每个精通
                                                                                                                        一种方言
                                                                                                                                                                                                                                                                      api: "anthropic-messages"                        api: "openai-completions"                       api: "google-generative-ai"                   api: "bedrock-converse"


                                                                                                                                      自己解析 SSE 流                  用官方 SDK                                   content → parts               包了 AWS 一层

                                                                                                                                                                                                                                                               content_block → text_*                            chunk.delta → text_*                                 thinkingConfig                             cachePoint 节点

                                                                                                                                                                                                                                                         ~1000行                             SDK帮你做脏活                    字段名都不同                 还有5+个





                                                                                                                                                                                                                                                                                                 AssistantMessageEventStream



                                                                                                                                                                                                 INSIGHT    前台不懂外语，翻译员不懂公司流程——两者靠"标准报告"协议连接。AgentLoop永远只看到统一事件流。

                                                                                         图：pi-ai 统一 API 与各家「方言」





 你不用记每家 API——这就是「中间层」存在的意义。你学过的适配器模式、策略模式，在工业代码里就是这么长的。

---

<!-- page 32/39 -->

原理  · 消息系统
模型「记得住」的东西

  LLM 只认识三种角色：user / assistant / toolResult
                                                                                                                                                                                                                                                                                                                                                                                UNION TYPE

                                                                                                                                                                                   AgentMessage

                                                                                                                                                                                                                                                                                                     context.messages 数组   assistant 消息里可以带文本 + 工具调用；每条消息带时间戳

  内部两层结构：发给模型的是「LLM 消息」，落盘存的是「会话

                                                                                                                               Message（标准消息）                                                       CustomAgentMessages

                                                                                                                                                                                                                                                                                 LLM 直接认识                                                                                                   LLM 不认识 · 内部状态 条目」——同一份事实，两种表示

                                                                                                                                                                                                                                                UserMessage            AssistantMsg           ToolResultMsg                                                        BashExecutionMsg                   CompactionSummary

                                                                                                                                                                                                                                          文本                                                                                                                                                                                                                                                                                                                   command                                                                                                                                                                                                                                                                                                                                                                                              +                                                                                                                                                                                                                                                                                                                                                                                                                output                                                                                                                                                                                                                                                                                                                                                                          summary
                                                                                                                                                                                                                                                                                                                                               role:                                                                                                                                                                                                                                                                                                 "user"                  role:                                                                                                                                                                                                                                                                                                                                                  "assistant"              role:                                                                                                                                                                                                                                                                                                                                                                   "toolResult"
                                                                                                                                                                                                                                     压缩后的旧对话                                                                                                                                                                                                                                                                                                                                                                                    +                                                                                                                                                                                                                                                                                                                                                                                                                    exitCode                                                                                                                                                                                                                                                                                                                                                                                             +                                                                                                                                                                                                                                                                                                                                                                                                                                              cancelled 模型没有跨调用的记忆——它每次「记得」的，就是你这次喂进                                                                                                                                             用户输入                                                                                                                                                           模型回复                                                                                                                                                                         工具结果
                                                                                                                                                                                                                                                                                                                                                                                         + truncated                                                                                                                                                                                                                                                            第9章详讲
                                                                                                                                              截图等                  + ToolCall                    isError 标记
                                                                                                                                                                                                                                                                                                                                                                    BranchSummary                      CustomMessage 去的列表                                                                                                                                                                                           被放弃分支的               扩展自定义

                                                                                                                                                                                                                                                              "遗言"摘要               通过声明合并

                                                                                                                                                                                                                                                      第10章详讲                注入类型

                                                                                                                          ↑ Agent 内部（7 种）                                      ✗仅excludeFromContext=true时过滤               ✗翻译成↓UserLLM 看到的（3 种）

                                                                                                                                                                                                                                                                                          convertToLlm() 翻译边界





                                                                                                                                                                                                                                                                                                                                                                  LLM CONTEXT

                                                                                                                                                                                                              Message[]（3 种）

                                                                                                                                                                                                                                                                                      UserMessage · AssistantMessage · ToolResultMessage





                                                                                                                                                                                   PRINCIPLE · 内富外严

                                                                                                     内部用 7 种消息类型自由表达 → LLM 边界翻译成 3 种标准格式

                                                                                                                                                                                                               Agent 能记录自己的内部状态（压缩、执行详情、分支），LLM 不需要认识这些——它只懂标准协议。

                                                      图：两层消息结构与三种角色




 「记忆」是个错觉：上下文就是记忆。这句话是理解 Agent 一切设计（压缩、截断、会话树）的总开关。

---

<!-- page 33/39 -->

原理  · 事件驱动
神经系统：事件

  循环里每发生一件事就广播一个事件：agent_start/end、                                                               agent_start
   turn_start/end、message_start/end、                                                                               Layer 1 · Agent（整个运行）                                                                                                               agent_start → agent_end


                                                                                                                                                                                                                                                                                          turn_start
   tool_execution_start/end……共 10 种                                                                                               Layer 2 · Turn 1                                                                                                                                          turn_start → turn_end





                                                                                                                                   →                                                                                                                                                                                                                                               message_start                                                                                                                                                                                                                                   message_end                                       message_start → message_update ×N → message_end  事件是嵌套的：agent ⊃ turn ⊃ message / tool，像 HTML 标                                        LAYER 3 · Message (User)                                                            LAYER 3 · Message (Assistant)
                                                                                                                                             （用户消息无                                                                                                                                                                                                                                               update）

                                                                                                                                                                                                                                                                                                                                                                                              LAYER 4 · TOOL EXECUTION

                                                                                                                                                                                                                                                                                                                                                                                                  tool_execution_start 签                                                                                                                                                                               → tool_execution_update ×N



                                                                                                                                                                                                                  → tool_execution_end


                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    turn_end  两类监听者：CLI 界面渲染 + 你的扩展代码
   （session.subscribe）                                                                                                                                       Layer 2 · Turn 2                                                                                                                                          turn_start → turn_end
                                                                                                                                                                                                                                                     LAYER 3 · Message (Assistant · stopReason = stop)

                                                                                                                                                                                                                                                                             message_start → message_update ×N → message_end

                                                                                                                                                    （没有ToolCall，所以没有Layer4嵌套） 订阅事件就能做：调用日志、超时告警、自动 git 提交、自定义




                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    turn_end  UI……





                                                                                                                                                                                                                                                                                        agent_end


                                                                                                                                                                                                 PATTERN  每层都是 "开始 → 更新（×N）→ 结束" 配对


                                                                                                                                                                                                                                                   Trace 边界事件（2 种：start/end）                    Turn 边界（2 种）                     Message 三连（3 种）                    Tool Exec 三连（3 种）         =共10种

                                                                                                                                 消费者按需订阅：UI只关心Layer3、性能监控看Layer2、工具调试看Layer4。

                                                        图：事件的嵌套层次




  想知道「Agent 此刻在干嘛」？订阅事件即可——相当于给 Agent 装了个外卖追踪。发布-订阅模式，你在 Web 课里学过，这里它负责可观测性。

---

<!-- page 34/39 -->

原理  · 上下文工程
固定窗口，无限对话

  窗口是死的（200K / 128K），对话是活的——矛盾怎么解？两
                                                                                                                                                                                                                   LLM 上下文窗口（固定）                                                                    CODE-AGENT 对话（无限增长）

                                                                                                                                                                                                                                             Claude 200K · GPT 128K · 硬上限                                     系统提示词 + 工具结果 + 历史 + 新输入 层防护
                                                                                                                                                                                                                                                                                         ↯ 超 → prompt is too long



                                                                                                      输入侧 · 送进 LLM 之前                                                                                                                                                           EACH PROMPT 输入侧：每次送进模型前，先截断工具输出（行数 + 字节双重限




                                                                                                                             ① 减法                                               ② 加法 制，先触者胜；留头或留尾两种策略）
                                                                                         工具输出截断                              系统提示词组装

                                                                                                                                                                                                                                          truncateHead / truncateTail                                                            CLAUDE.md 向上递归 + Skills 懒加载

                                                                                                                                                                                                                                2000 行 / 50KB 双重限制                                                             XML 包装让 LLM 理解项目规范 轮次侧：两轮对话之间检查总量，超线就做压缩                                            单条bash/read/grep结果太大就裁                               让LLM自动知道"用pnpm不用npm"

 系统提示词 + 工具定义 + 历史 + 新输入，全挤在这一个窗口


                                                                                                      历史侧 · 长对话管理                                                                                                                                                       THRESHOLD / BRANCH ——上下文就是 Agent 的工作记忆



                                                                                                                             ③ 压缩（独立成章 · 第 9 章）                                   ④ 加法 · LCA 算法

                                                                                                                                                     Compaction                                                   Branch Summarization

                                                                                                                                                                                               阈值触发：contextTokens > window − reserve                             用户切换会话树分支时触发

                                                                                                                             旧消息 → 6 section 结构化摘要                                        给被抛弃分支生成精简摘要（2048 tok）

                                                                                                                                       线性对话过长时腾出空间                                           让新分支知道旧分支试过什么





                                                                                                                           减法 · 把太大的东西变小             加法 · 主动加入结构与摘要                没有银弹，只有层层兜底

                                                        图：两层防护全景

---

<!-- page 35/39 -->

原理  · 上下文工程
窗口快爆了，具体怎么做？

  触发判断：contextTokens > 窗口 − reserve ，红灯亮起                                      ──── 当前运行 ────                                                        ─ 下次运行 ─





                                                                                                                                                                                                                       STEP 1                         STEP 2                         STEP 3                         STEP 4                         STEP 5                       STEP 6 选一个切点：旧的部分交给模型写摘要，新的部分原样保留

                                                                                                    触发判断           找切割点           分割消息          生成摘要 ★         存储 Entry         下次重建



                                                                                                                                                                            调                                                                                                                                         从新→旧累积                                                                                                                                                                                                                                      contextTokens                                                                                                                                                                                                                                   >                                                                                                                                                                                                                                                                             token               messagesToSummarize                                                                                                                                                                                                                                                                                  LLM 生成                       CompactionEntry                                                                                                                                                                                                                                                                                                                                                                                                                                 buildSession 摘要替换旧消息回到上下文，继续干活                                                                                                                                                                            结构化摘要                                                                                                                                                                                                                     contextWindow                                                                                                                                                                                                                                                                                                                                                                            -                                                                                                                                                                              → 旧消息                                                                                                                                         跳过 toolResult                                                                                                                                                                                                                                                                                                                                                                                                                                                     Context()                                                                                                                                                                                              追加到 Session Tree
                                                                                                                                                                                                                                        reserveTokens?                                                                                                                                         找最近合法位置                                                                                                                                                                                                                                                                                                                                                             6                                                                                                                                                                                                  个 section：             不立即改                                                                                                                                                                                                                                                                                                                                                                                                                         context            遍历路径                                                                                                                                                                                                                                                                                                            kept                                                                                                                                                                                                                                                                                                                                                                                                                 Goal/Constraints/
                                                                                                                                                                                                                                          遇                                                                                                                                                                                                                                                                                                                                                                                                                                             CompactionEntry                                                                                                                                                                              → 保留消息                                                                                                                                                                                                                                                                           shouldCompact()                                                                                                                                                                                                                                                                                                                                                                                                            Progress/Decisions/
                                                                                                                                                                                                                                                                                                                   findCutPoint()                                                                                                                                                                                                                                          →                                                                                                                                                                                                                                           替换旧消息                                                                                                                                                                                                                                                                                                                                                                                                                                                  Next/Critical                                                                                                                                                                                                                                                                                                                                                                                                                 emit 有损但可回：完整原文永远在 JSONL 里，/tree 随时翻回去                                     emit                                                                                                                                                          处理Turn分割                          + previousSummary                       compaction_end          ★跨运行时
                                                                                                                                                                                                                                                                           compaction_start





                                                                                                                                                                                                      KEY INSIGHT



                                                                                                                                                                         CompactionEntry 是连接两次运行的桥梁

                                                                                                            压缩时：在 Session Tree 上追加一个 CompactionEntry 节点（记录 cutPoint、summary、kept messageIds）

                                                                                                                                                                                   下次启动时：buildSessionContext() 遍历从 leaf 到 root 的路径

                                                                                                            → 遇到 CompactionEntry → 生成 CompactionSummaryMessage 替换旧消息 → 之后的消息正常 push


                                                                                                                                                                 关键：context不立即改变，而是"下次构造时再生"。这让压缩本身是无副作用的——失败了大不了不压缩。

                                                                                                                                                                                                      CompactionSummaryMessage通过convertToLlm翻译成<summary>标签的UserMessage发给LLM。


                                                        图：压缩触发与流程





 摘要是整个 AI 工程里最「要命」的动作——摘要错了，后面全盘跑偏。pi 的答案是：自动做，但留全量备份。这个思路值得抄进你自己的项目。

---

<!-- page 36/39 -->

原理  · 会话管理
分叉 = 挪一个指针

  每条记录：id + parentId + 内容，JSONL 逐行追加
                                                                                                             ① 当前状态                                             leafId = A2      ② 用户回退到 A1                                       leafId = A1      ③ 从 A1 长出新分支                           leafId = B2



                                                                                                                                                                                                                                                                   root                                                                    root                                                                root   leafId = 当前走到哪个节点——回退就是改这个指针，文件一个
 字节都不动                                                                                                                                                                                    A1                                                          A1                                                       A1





                                                                                                                                                              用户回退                                  追加新消息   /tree 跳跃、/fork 分叉、/clone 克隆，全建立在「树 + 叶子指                                     A2                   A3                                         A2                   A3                                      A2        B1        A3



                                                                                                                                                                                            ↑ leafId 针」上                                                                                                                           ↑ leafId

                                                                                                                                                                                                                                                                                                                                    B2
 所以回退 / 分叉几乎免费：O(1)                                                                说明                                   说明                                                  ↑ leafId
                                                                                                                        用户走到 A2，                               回退 = 移动 leafId，
                                                                                                                        这是当前分支末端。                                  不删任何节点（O(1)）
                                                                                                                                                                                    路径：root→A1→A2                                          A2仍在树上，只是被抛弃                           说明
                                                                                                                                                                                                                        从 A1 长出新分支 B，
                                                                                                                                                                                                                                                                                                                                                                    B1 → B2 是新对话。




                                                                                                                                                                                                                  APPEND-ONLY 铁律


                                                                                                                                                                                                                                                                  leafId 位置           新分支             被抛弃分支               普通节点
                                                                                                                                      当前对话走到哪           从分叉点长出的新路径        仍在树上，永不删除             在当前路径上的中间节点


                                                                                                                  三种操作都是O(1)：追加（写新节点+移leafId）、回退（只移leafId）、重试（回退+追加）

                                                                                               →走错路了就分叉，不用开十个终端窗口。所有分支活在同一个文件里。

                                                       图：会话树与叶子指针





 本质很简单，包装做得好——「好的设计是把复杂留给自己」的教科书案例。数据结构课上讲的树，在工业代码里就这么落地。

---

<!-- page 37/39 -->

原理
发动机室参观完毕

    1 循环：模型调用 ↔ 工具执行；「不再调用工具」= 停

    2 工具：名片 + 执行；五步流水线；「一票否决」串行

    3 模型：统一 API 翻译层，一行 streamSimple 驾驭 30+ 家

    4 消息：三种角色、两层表示；上下文就是记忆

    5 事件：10 种嵌套事件，扩展的挂点、可观测性的来源

    6 上下文：截断 + 压缩两层防护；压缩自动且可回

    7 会话：JSONL 树 + 叶子指针，回退免费


 一句话收束：极简内核 + 开放的扩展点 = 可驾驭的外壳。

---

<!-- page 38/39 -->

课后作业
作业（三选一，进阶选二）


 ① 用起来                  ② 画出来                  ③ 造一个
 安装 pi，在自己的课程项目里让它干一件小事（比   不看书自己画一张 Agent Loop 图（Trace / Turn   写一个 20 行以内的扩展：注册一个 /hello 命令或
 如生成 README），记录底栏的 token 用量和花        / steering），课上 1 分钟讲给同学听             一个日志工具（examples/extensions 目录有模
 费，写 3 句「值不值」                                        板）


 加餐题：用 pi 分析这套 PPT 自己的源码——让它给你讲讲第 32 页的事件系统。它讲得比你好，说明你该重学一遍了。

---

<!-- page 39/39 -->

延伸阅读
想深挖，去哪儿

   pi.dev 与 pi.dev/docs：官网演示 + 完整文档（providers、                                   学习路线建议          一句话
  extensions、sdk……）                          会装会用（今天）→ 读第 3、5 章   最好的学习方法：让 pi 给你讲 pi
                                        原理笔记 → 读            ——它对自己的解释，就是这套
  GitHub：earendil-works/pi-mono：源码 + 50+ 官方扩展示
                                                                            examples/extensions → 写自己      PPT 的另一种写法
   例（examples/extensions）                                        的第一个扩展
   Discord 社区：问题、案例、新包第一时间在这

  本课配套笔记：dg-ai-notes——pi_source_dive 11 章源码详解
  + pi_sdk_learn 7 章二次开发实战（含配图）

    AGENTS.md ：pi 仓库自带的「班规」——AI 时代怎么给 Agent
 定规矩，现成的范例
