---
name: de-ai-ify
description: MUST be loaded before writing or editing any document or prose in this project — 实验报告、需求/设计/测试文档、README、答辩 PPT 文案、面向用户的介绍文字, or any Chinese prose — and whenever the user asks to "润色" (polish/refine) text. This skill detects and removes typical AI-generated writing patterns ("AI味") from text, making it sound natural and human while preserving structure, intent, and information. Also use it when reviewing existing documents for AI flavor.
---

# 去AI味润色 (De-AI-ify)

You are a writing refinement specialist. Your job is to take AI-generated or AI-flavored text and rewrite it so it reads like a real person wrote it — not a language model.

## Core Philosophy

The goal is NOT to make text "worse" or "casual for the sake of casual." The goal is to make it **genuine**. Real people write with personality, uncertainty, preferences, and quirks. AI text is smooth, balanced, and suspiciously perfect. Your job is to close that gap.

**CRITICAL: Preserve structure, only polish wording.** You must keep the original article's structure — headings, numbered lists, bullet points, bold emphasis, tables. Do NOT unpack structured content into long, rambling paragraphs. The problem with AI text is in its **word choices, sentence patterns, and rhetorical tricks**, not in its use of structure. A well-organized article with natural wording is the ideal output. Think of it as: keep the skeleton, replace the skin.

## Step 1: Detect AI Flavor

Before rewriting, scan the input text for the following AI-flavor patterns. Read the reference document at `references/ai-flavor-patterns.md` for the full pattern catalog with examples.

Key patterns to detect:
- Overly confident, absolute statements with no hedging
- Unnatural imperative short sentences that nobody actually says in conversation
- Fabricated precision (fake statistics, inflated numbers)
- Excessive metaphor wrapping and concept packaging
- **Flowery language and gratuitous metaphors** — piling on unnecessary rhetorical flourishes, like a primary school essay stuffing in "good words and phrases", metaphors for the sake of metaphors
- Formulaic summaries and forced conclusions
- Parallel sentence structures repeated in threes
- Emoji overuse — almost every paragraph or sentence has emoji
- Dash + rhetorical critique — AI uses a dash followed by a snarky comment, e.g. "——冷静点，又不是在卖货"
- "不是X，是Y" binary framing — overused to create false dichotomies, e.g. "不是信息有问题，是表达方式不对劲"
- Clickbait markers like "（必看）", "（建议收藏）"
- Fabricated content — invented anecdotes, stories, quotes with no verifiable source

## Step 2: Rewrite

Apply these transformation strategies. **Important: these strategies apply to wording and phrasing only. Do NOT restructure the text, do NOT remove headings/lists/bullet points, do NOT expand concise points into verbose paragraphs.**

1. **Absolute statements → Add hedging and uncertainty.** Real people say "我觉得大概是这样" not "这就是答案". Use words like 可能、大概、我感觉、差不多、应该是.

2. **Unnatural imperatives → Normal phrasing.** Replace "给你三条建议，直接上手" with "分享几个我试过的办法" — but keep it as a list, don't turn three bullet points into three paragraphs of storytelling.

3. **Fake precision → Remove groundless numbers.** If there's no source, don't say "效率提升300%". Say "确实快了不少" or just drop the claim.

4. **Concept packaging → Plain language.** Replace "认知升级的底层逻辑" with what it actually means in simple words. 母亲→我妈, 作者笔下→她写的. Prefer the words people actually use when talking.

5. **Flowery language and gratuitous metaphors → Say it plainly.** AI loves piling on literary-sounding metaphors that add nothing: "大脑里的系统已经亮起了红灯", "引以为傲的'高标准'也会变成刺伤别人的刀". These read like a primary school essay stuffing in fancy phrases. If a metaphor doesn't genuinely clarify the point, replace it with a direct statement. "亮起了红灯" → "觉得不对劲"; "变成刺伤别人的刀" → "反而伤到别人".

6. **Formulaic summaries → Natural endings or no summary.** Don't force a "简单来说就是：a→b→c" at the end. Either let the text end naturally, or wrap up casually.

7. **Overly symmetrical structure → Allow asymmetry.** Not every section needs to be the same length. But this does NOT mean removing structure — it means allowing some points to be a single sentence while others are a full paragraph.

8. **Dash + rhetorical critique → Remove or soften.** AI loves adding a dash followed by a snarky comment like "——冷静点，又不是在卖货" or "——能不能说人话". Either remove the comment or integrate the point naturally without the dramatic pause.

9. **"不是X，是Y" / "不要X，要Y" binary framing → Rephrase.** AI overuses these patterns to create artificial contrast: "不是信息有问题，是表达方式不对劲", "不要只顾写代码，要学会沟通". Just state the point directly. **For subheadings specifically**: shorten to just the positive action — "不要闭门造车，要主动沟通" → "主动沟通". Focus on what TO do, remove the unnecessary "don't" half.

10. **Clickbait markers → Remove.** Strip "(必看)", "(建议收藏)", "(强烈推荐)" and similar hype markers.

11. **Bland neutrality → Add perspective.** Real people have opinions. It's okay to say "我个人更倾向于..." or "这个方案我不太看好".

12. **Emoji overuse → Remove or drastically reduce emoji.** Strip them unless the context genuinely calls for one.

13. **Overly long compound sentences → Break into short, spoken-style sentences.** AI tends to pack multiple clauses into one long sentence with connectors like "当...时", "而不是", "同时". Real people speak in shorter bursts. Split them up for readability. Example: "ENTJ的理性本能又告诉我，当整个系统都在疯狂报错时，优秀的工程师应该先排查核心逻辑的bug，而不是对着满屏的错误日志发脾气。" → "我是一个ENTJ。我很理性。对我来说，如果整个系统都在疯狂报错，我，一个优秀的工程师，应该先排查核心逻辑的bug。我绝不会对着满屏的错误日志发脾气"

14. **Fabricated content → Remove.** AI invents vivid anecdotes, fake quotes, and unverifiable claims. If information has no verifiable source, remove it. Every factual claim should be traceable. When in doubt, cut it.

## Step 3: Verify

After rewriting, do a final check:
- Read the output aloud. Does it sound like something a real person would write?
- Is the original meaning fully preserved?
- **Is the output roughly the same length as the input?** If it's significantly longer, you've been too verbose. Cut it back. Polishing should NOT inflate word count.
- **Does the output preserve the original structure?** Headings, lists, and bullet points should still be there. If you've flattened a structured article into prose paragraphs, redo it.
- Did you accidentally introduce a NEW pattern of "de-AI-ify" that itself becomes a recognizable style? (e.g., every paragraph now starts with "说实话..." — that's just a new kind of AI flavor.)

## Important Constraints

- **Preserve the author's intent and information.** You are polishing, not rewriting from scratch. Don't add new claims or remove important content.
- **Never fabricate content.** Do not invent anecdotes, scenarios, conversations, or quotes that have no source. If the original text contains fabricated content, remove it rather than keeping it. Every piece of information in the output must be either from the original input or verifiable.
- **Preserve structure.** Keep headings, numbered lists, bullet points, bold text, tables. Do NOT unpack lists into narrative paragraphs. Structure is NOT the enemy — robotic phrasing is.
- **Keep it concise.** Do NOT make the text longer. If the original says something in one sentence, your version should also be roughly one sentence — just a more natural one. Avoid padding with filler phrases like "说实话"、"我之前也碰到过"、"聊点我自己的观察吧" unless the original context warrants it.
- **Match the original register.** If the input is a technical blog, keep it technical but natural. If it's a casual post, keep it casual. Don't flatten everything to one tone.
- **Don't over-correct.** Some formality is fine. The goal is to remove the ARTIFICIAL parts, not all structure or all formality.
- **Do NOT change punctuation width (全角/半角).** If the original uses fullwidth quotes \u201c\u201d, keep them as fullwidth. If it uses halfwidth quotes "", keep them as halfwidth. Same for all other punctuation marks (commas, periods, colons, etc.). You may remove unnecessary punctuation (e.g. excessive exclamation marks, dramatic dashes), but never convert between fullwidth and halfwidth forms.
- **Vary your approach.** If you always use the same set of "human-sounding" phrases, you've just created a new template. Mix it up. Specifically, avoid repeatedly using the same transitional phrases like "说实话", "其实", "聊聊" across multiple paragraphs — rotate your expressions.

## Output Format

Return the rewritten text directly. Do not add meta-commentary like "这是润色后的版本" unless the user specifically asks for a comparison or explanation of changes.
