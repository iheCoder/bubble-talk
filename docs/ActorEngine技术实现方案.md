# BubbleTalk ActorEngine 技术实现方案（Go｜语音原生｜“演员提示词构建”）

> ActorEngine 的职责是：把导演（DirectorEngine）输出的结构化计划 `DirectorPlan`，编译成 **发给 GPT Realtime 的 System Instructions 或 Session Update**（ActorPrompt），并确保它在工程上可校验、可回放、可兜底。
>
> 关键原则：**导演负责“拍”（选择拍点/动作/工具/约束），演员负责“构建人设与任务”（Prompt Construction），Realtime 负责“说”（生成音频）**。
>
> 关联文档：
> - 全局后端：`docs/第一阶段技术实现方案.md`
> - 编排器：`docs/会话编排器技术实现方案.md`
> - 角色固定调度：`docs/导演如何调度“泡泡角色”.md`

---

## 0. 第一阶段的 ActorEngine 要交付什么

第一阶段（MVP-1）ActorEngine 必须做到：

1. **Prompt 构建**：每轮输出一个 `ActorPrompt`（包含 Role Definition, Task, Constraints），用于驱动 GPT Realtime。
2. **严格结构与校验**：Prompt 必须包含必要的约束（如时长限制、禁止术语），防止模型放飞自我。
3. **语音原生友好**：Prompt 中必须显式要求模型“口语化、短句、多停顿”。
4. **工具协同**：当本轮被编排器注入 quiz/exit ticket 等工具时，Prompt 必须包含“引导用户看题/作答”的指令。
5. **角色受限**：必须遵守泡泡固定 `RoleSet` 与角色能力边界。

---

## 1. ActorEngine 的边界（最重要）

### 1.1 ActorEngine 做什么

- 把 `DirectorPlan` 翻译成 GPT Realtime 能理解的 `Instructions`。
- 把“输出动作”（OutputAction）转化为具体的 Prompt 指令（例如：“Ask the user to recap using 'because... so...' structure”）。
- 控制输出节奏（通过 Prompt 约束 talk burst 和句子长度）。
- 在失败时提供确定性兜底（虽然 Realtime 是流式的，但 Prompt 构建本身是确定性的）。

### 1.2 ActorEngine 不做什么（禁止越权）

- 不负责决定“下一拍点是什么”（那是导演）。
- 不负责直接生成最终的台词文本（那是 GPT Realtime 的工作）。
- 不负责决定“要不要出题/出哪题”（那是导演 + 编排器 + Assessment）。
- 不负责“把用户带偏到新主线”（主线只由导演维护）。

> 经验法则：如果某一步会改变学习路线/主目标，那就不应该在 ActorEngine 里做。

---

## 2. 输入/输出契约（Contract）

ActorEngine 的工程可控性来自“契约”，而不是 prompt 祈祷。

### 2.1 输入：ActorRequest（由编排器组装）

建议由编排器传入一个结构化输入（示意）：

```go
type ActorRequest struct {
  SessionID string
  TurnID    string

  // 导演计划（已过 Guardrails 修正）
  Plan DirectorPlan

  // 泡泡与概念骨架（防胡编/防跑题）
  EntryID       string
  Domain        string
  MainObjective string
  ConceptPack   ConceptPackSummary

  // 角色固定：本轮只能从 RoleSet 中选一个 target_role
  RoleSet       []RoleProfile
  TargetRoleID  string // == Plan.NextRole（必须在 RoleSet 内）

  // 上下文：只给“摘要”，不给全聊天记录
  Context ActorContext

  // 工具注入：由编排器决定，本轮需要宣读/引导的工具内容
  ToolPayload *ToolPayload

  // 语音策略：时长/可打断/语速等（第一阶段可简化）
  VoicePolicy VoicePolicy
}
```

其中：

- `ConceptPackSummary` 至少包含：`core_relation`、`misconceptions(top2)`、`boundaries(top1)`、`transfer_targets(top2)`
- `ActorContext` 建议只包含：`last_user_text`、`last_assistant_text`、`recent_summary`、`output_clock_sec`、`misconception_tags`
- `ToolPayload` 的存在意味着“本轮要让用户作答/确认”，Actor 必须围绕它组织话术

### 2.2 输出：ActorPrompt（指令）

第一阶段建议输出结构化的 Prompt 字符串或对象：

```go
type ActorPrompt struct {
    // 发给 Realtime 的 instructions
    Instructions string 
    // 调试信息
    DebugInfo    map[string]interface{}
}
```

示例 Instructions 内容：
```text
[Role]
You are the "Host". You are energetic, encouraging, and speak in short, spoken-style sentences.

[Task]
The user is in "Fog" state (confused).
Your goal is to "Reveal" the concept using a simple metaphor.
Do NOT use jargon.
Use the "Coffee Shop" metaphor to explain Opportunity Cost.

[Constraints]
- Keep your response under 20 seconds.
- End with a simple question to check understanding.
```
```

---

## 3. 与导演/角色系统的一致性（避免文档打架）

`docs/导演如何调度“泡泡角色”.md` 强约束：

- **角色固定**：每个泡泡一个 `RoleSet`
- 导演只能在 RoleSet 内切换“谁说话”
- 角色能力（allowed_actions/allowed_stances）约束“这轮能怎么说”

ActorEngine 必须遵守：

1. 不得输出 RoleSet 之外的 `role_id`
2. 不得输出该角色不允许的 stance/语气（若你把 stance 显式化）
3. 不得输出“角色随意调用工具”的语句（例如“我给你出一道题”）除非 ToolPayload 明确指示本轮出题

建议：在 ActorPrompt 的 debug 中写入 `role_id + template_id`，便于排查“为何这轮像换了个人”。

---

## 4. ActorEngine 与 GPT Realtime 的集成

在新的简化架构下，ActorEngine 的核心职责是 **Prompt Construction（提示词构建）**。它接收导演的指令，结合角色人设，构建最终发给 GPT Realtime 的 System Instructions 或 Session Update。

### 4.1 构建流程

1. **接收 DirectorPlan**：获取当前 Beat、策略、目标角色、以及具体的指令（如“用比喻解释”、“进行反问”）。
2. **加载 Role Profile**：获取当前角色的 System Prompt（人设、说话风格、禁止事项）。
3. **组装 Context**：
   - **Role Definition**: "你是[角色名]，你的特点是..."
   - **Current Task**: "现在的策略是 [Beat Strategy]。用户处于 [UserMindState]。你需要执行：[Director Instructions]..."
   - **Constraints**: "回答必须在 20 秒内。不要长篇大论。"
4. **调用 Realtime API**：将组装好的 Prompt 作为 `session.update` 或 `response.create` 的 instructions 发送给 GPT Realtime。

### 4.2 示例 Prompt 结构

```text
[Role]
You are the "Economist". You are rigorous, logical, but sometimes dry.

[Current Situation]
User is in "Fog" state (confused).
Director Strategy: "RevealBeat" (Simplify and explain).

[Task]
Stop using jargon. Use the "Coffee Shop" metaphor to explain Opportunity Cost.
Keep it under 2 sentences.
Ask the user if they understand the metaphor.
```

---

## 5. 校验与修复（Validator/Repair）：让输出永远可控

虽然我们不能直接校验 GPT Realtime 生成的音频，但我们可以校验 **Prompt 本身**：

### 5.1 Prompt 校验

- **长度校验**：Prompt 不能过长，否则会增加延迟和 Token 消耗。
- **关键词校验**：确保 Prompt 中包含了当前 Beat 的核心指令（如 "Reveal", "Check"）。
- **角色一致性**：确保 Prompt 中的 Role Definition 与 `TargetRoleID` 一致。

### 5.2 兜底策略

如果 Prompt 构建失败（例如缺少 Concept Pack 数据），应回退到通用的“安全模式” Prompt：
- "You are a helpful tutor. The user is confused. Please explain the concept simply."

- 中文语速约 4~6 字/秒（含停顿）
- 估计时长 `sec ≈ len(汉字)/5 + 标点停顿`

若估计超限：

- 优先“截短 + 保留用户动作提示”
- 绝不允许为了补充解释而牺牲 OutputAction（否则 OutputClock 失效）

### 7.3 Repair 的边界（只做可控修复）

允许修复：

- 缺字段：补默认（例如 interruptible_after_ms=800）
- user_action.prompt 缺失：从 ActionCard 补一个模板
- speech_text 过长：压缩为两句 + 一个问题

---

## 6. 性能优化与延迟管理

由于 Prompt 构建和 Realtime 生成都存在延迟，必须采取优化措施：

### 6.1 Prompt 极简主义

- **只传 Diff**：不要每次都发送完整的 System Prompt。只发送 `session.update` 更新当前 Beat 的指令。
- **指令词化**：使用 "Command-like" 语言，而非自然语言。
  - *Bad*: "Please try to explain this concept using a simpler metaphor..."
  - *Good*: "Strategy: REVEAL. Action: Use 'Coffee Shop' metaphor. Max length: 2 sentences."

### 6.2 预加载与缓存

- 对于固定的开场白（ColdOpen）或转场（Transition），可以预先构建好 Prompt 甚至预生成音频，减少首字延迟。

---

## 7. 可观测性（让 ActorEngine 可调参、可验收）

建议每轮记录以下信息（写入 timeline 或日志/metrics）：

- `actor.prompt_length`（Prompt 长度，监控 Token 消耗）
- `actor.strategy`（当前使用的 Beat Strategy）
- `actor.latency_ms`（构建 Prompt 的耗时）
- `realtime.response_latency_ms`（从发送指令到收到第一个音频分片的耗时）

这些指标能直接告诉你：
- 为什么会出现“长讲压声”
- 为什么 OutputClock 失效（没有 user_action）
- 哪些 Prompt 导致模型“不听话”

---

## 8. 测试策略（第一阶段就该有）

### 8.1 单元测试（不依赖模型）

- 给定固定 `ActorRequest`，应生成符合预期的 `ActorPrompt` 字符串。
- 校验 Prompt 中是否包含必要的 Role/Task/Constraints 关键词。

### 8.2 契约测试（与编排器联调）
- 确保编排器传入的 `DirectorPlan` 能正确映射到 Prompt 中的指令。
- 确保 `ToolPayload` 能正确转化为 Prompt 中的“引导语”。

- `DirectorPlan -> ActorRequest -> ActorReply` 的字段映射不应漂移
- tool payload 注入时，ActorReply 必须包含“宣读+指令”

---

## 13. 第一阶段落地清单（可直接拆任务）

1. 定义 `ActorRequest/ActorReply` 的 Go struct + JSON schema（白名单）
2. 实现 TemplateLibrary（BeatCard/ActionCard/RoleProfile）
3. 实现 TemplateRenderer（纯模板路径）并接入 Validator
4. 实现 FallbackGenerator（覆盖核心 OutputAction）
5. 在编排器里把 `ActorReply` 写入 Timeline（assistant_text + user_action + debug）
6. 预留 LLM 生成路径接口（但第一阶段不强制接入）

