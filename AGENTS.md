# AGENTS.md

## 项目背景

UESTC - 软件开发系统综合实验期末大作业项目。目标是实现一个数据备份软件，参考 Duplicati 实现

- 评分规则与裁剪顺序：`docs/课程期末要求速查.md`、`docs/得分点清单.md`
- 目标平台：GUI 客户端 Windows 为主，服务端部署 Linux（docker）
- 本地备份 CLI 已经可用：打包、还原、校验，Huffman 和 LZ77 压缩，AES 加密，任务。目前的目标是仿照 Duplicati 做客户端图形界面

## 项目规范

- 扩展功能靠第三方库直接实现，该项分数减半；Huffman、LZ77、打包格式必须自己写。
- 完整性校验用 CRC-64/ECMA（`hash/crc64`），多项式不能换。压缩必须在加密之前。
- 写 PR 说明、提交信息和大段注释时只写代码做了什么、保证了什么；不写看代码就懂的废话，也不写自己的操作过程，只提供“不说明后续开发者就不见得能知道”的信息
- 开发的工作流程完整遵守 `docs\编码工作流.md`

## Guidelines

1. Think Before Coding
Don't assume. Don't hide confusion. Surface tradeoffs.

Before implementing:

State your assumptions explicitly. If uncertain, ask.
If multiple interpretations exist, present them - don't pick silently.
If a simpler approach exists, say so. Push back when warranted.
If something is unclear, stop. Name what's confusing. Ask.
2. Simplicity First
Minimum code that solves the problem. Nothing speculative.

No features beyond what was asked.
No abstractions for single-use code.
No "flexibility" or "configurability" that wasn't requested.
No error handling for impossible scenarios.
If you write 200 lines and it could be 50, rewrite it.
Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

1. Surgical Changes
Touch only what you must. Clean up only your own mess.

When editing existing code:

Don't "improve" adjacent code, comments, or formatting.
Don't refactor things that aren't broken.
Match existing style, even if you'd do it differently.
If you notice unrelated dead code, mention it - don't delete it.
When your changes create orphans:

Remove imports/variables/functions that YOUR changes made unused.
Don't remove pre-existing dead code unless asked.
The test: Every changed line should trace directly to the user's request.

1. Goal-Driven Execution
Define success criteria. Loop until verified.

Transform tasks into verifiable goals:

"Add validation" → "Write tests for invalid inputs, then make them pass"
"Fix the bug" → "Write a test that reproduces it, then make it pass"
"Refactor X" → "Ensure tests pass before and after"
For multi-step tasks, state a brief plan:

1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.
