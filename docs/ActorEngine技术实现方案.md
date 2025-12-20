# BubbleTalk ActorEngine 技术实现方案（Go｜语音原生｜“演员执行分镜”）

> ActorEngine 的职责是：把导演（DirectorEngine）输出的结构化计划 `DirectorPlan`，编译成 **可朗读、可打断、可工具化的台词脚本**（ActorReply），并确保它在工程上可校验、可回放、可兜底。
>
> 关键原则：**导演负责“拍”（选择拍点/动作/工具/约束），演员负责“说”（按指令表达）**。ActorEngine 不能“自由发挥去当导演”。
>
> 关联文档：
> - 全局后端：`docs/第一阶段技术实现方案.md`
> - 编排器：`docs/会话编排器技术实现方案.md`
> - 角色固定调度：`docs/导演如何调度“泡泡角色”.md`

---

## 0. 第一阶段的 ActorEngine 要交付什么

第一阶段（MVP-1）ActorEngine 必须做到：

1. **脚本化输出**：每轮输出一个 `ActorReply`（短句、口语化、可朗读），包含用户必须完成的动作（recap/choice/transfer…）。
2. **严格结构与校验**：ActorReply 必须通过 schema 校验；不合格必须走兜底脚本（fallback），不能把“乱输出”透传给用户。
3. **语音原生友好**：输出以“说出来”的体验为第一优先（短句、停顿自然、避免长段落），并提供可打断点（interruptible_after_ms）。
4. **工具协同**：当本轮被编排器注入 quiz/exit ticket 等工具时，ActorReply 必须能“宣读题干 + 指导作答”，但 **ActorEngine 不得自行决定出题**。
5. **角色受限**：必须遵守泡泡固定 `RoleSet` 与角色能力边界（不能临时发明 Challenger/Coach 等角色）。

---

## 1. ActorEngine 的边界（最重要）

### 1.1 ActorEngine 做什么

- 把 `DirectorPlan` 翻译成可执行的“台词脚本”
- 把“输出动作”（OutputAction）落到一句可执行指令（例如：让用户复述/选择/举例/迁移）
- 控制输出节奏（talk burst、短句、避免长讲）
- 在失败时提供确定性兜底（模板化脚本）

### 1.2 ActorEngine 不做什么（禁止越权）

- 不负责决定“下一拍点是什么”（那是导演）
- 不负责决定“要不要出题/出哪题”（那是导演 + 编排器 + Assessment）
- 不负责更新掌握度/误解标签（那是编排器归约与 Assessment）
- 不负责“把用户带偏到新主线”（主线只由导演维护）

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

### 2.2 输出：ActorReply（脚本）

第一阶段建议用严格 JSON：

```json
{
  "role_id": "Host",
  "speech_text": "……（短句、口语化、可朗读）",
  "interruptible_after_ms": 800,
  "user_action": { "type": "recap", "prompt": "用一句话复述，必须包含因为…所以…" },
  "quiz": null,
  "fallbacks": [
    "如果你卡住，就用这个句式：因为____，所以____。"
  ],
  "debug": {
    "beat": "Check",
    "output_action": "Recap",
    "template_id": "check.recap.v1"
  }
}
```

约束要点：

- `role_id` 必须等于 `TargetRoleID`（不允许演员擅自换人）
- `speech_text` 必须可朗读（不要 markdown、不要代码块、不要长列表）
- `user_action.type` 必须与 `Plan.OutputAction` 一致（或是其允许的等价映射）
- `interruptible_after_ms` 必须存在（语音原生的“可打断点”）
- `quiz` 只能来自 `ToolPayload`（ActorEngine 不得自行生成题目）

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

建议：在 ActorReply 的 debug 中写入 `role_id + template_id`，便于排查“为何这轮像换了个人”。

---

## 4. 模块划分（实现层）

ActorEngine 建议拆为 6 个子模块（第一阶段可合并，但边界要清晰）：

1. **TemplateLibrary**：BeatCard / ActionCard / RoleStyle 模板库（JSON/YAML）
2. **PromptBuilder**：将 ActorRequest 编译成模型输入（或直接走模板渲染）
3. **Generator**：三种生成策略之一（见 5.1）
4. **ResponseParser**：解析模型输出 JSON（或解析模板渲染结果）
5. **Validator/Repair**：硬校验 + 可控修复（不通过则兜底）
6. **FallbackGenerator**：确定性兜底脚本（必须覆盖所有 OutputAction）

---

## 5. 生成策略（第一阶段推荐路径）

### 5.1 三种可选路径

ActorEngine 可以按成熟度分三档：

**A. 纯模板（最稳，第一阶段推荐先落地）**

- `DirectorPlan + ConceptPack + ToolPayload -> 模板渲染 -> ActorReply`
- 优点：确定性强、可测、可控；缺点：表达力一般

**B. 文本模型生成脚本（推荐第二步引入）**

- 用“文本模型”生成 ActorReply JSON（严格 schema）
- 优点：表达力更自然；缺点：需要解析/校验/兜底

**C. 复用 gpt-realtime 的文本能力生成脚本（可行但更难控）**

- 用 Realtime 生成 text + audio
- 难点：要把“系统裁决/工具注入/硬约束”牢牢握在编排器侧，否则会演化成“模型自走棋”

第一阶段建议：**A 起步 + 为 B 预留接口**。

### 5.2 为什么第一阶段不建议直接让模型自由生成长文本

因为你要验收“90 秒必输出”“退出必迁移”，最怕的不是“不够聪明”，而是“不可控”：

- 模型容易把“让用户输出”弱化成陪聊
- 容易输出过长，语音体验崩（用户打断多，节奏失控）
- 容易越权调用工具/改变主线

---

## 6. 模板库（TemplateLibrary）如何设计成 DSL

### 6.1 模板的层级

建议三层模板：

1. **Beat 模板（BeatCard）**：这轮在“剧情结构”上要做什么（Check/Twist/Transfer…）
2. **OutputAction 模板（ActionCard）**：这轮必须逼用户做什么（Recap/Choice/Example/Feynman/Transfer）
3. **RoleStyle 模板（RoleProfile）**：这个角色“怎么说话”（口吻、禁忌、常用句式）

最终脚本 = `RoleStyle` + `BeatCard` + `ActionCard` + `ToolPayload`

### 6.2 推荐配置形式（示意）

`configs/beat_library.json`（示意片段）：

```json
{
  "Check": {
    "goal": "立刻验证是否听懂",
    "do": ["ask_one_question", "force_user_output"],
    "avoid": ["long_lecture"]
  },
  "Twist": {
    "goal": "打误解/制造认知冲突",
    "do": ["name_the_mistake", "give_counterexample"],
    "avoid": ["introduce_new_main_concept"]
  }
}
```

`configs/action_library.json`（示意片段）：

```json
{
  "Recap": {
    "user_action_type": "recap",
    "prompt_templates": [
      "用一句话复述，必须包含因为…所以…",
      "用“如果…那么…”说出关键关系"
    ]
  },
  "Transfer": {
    "user_action_type": "transfer",
    "prompt_templates": [
      "把这个概念用到一个新场景：{transfer_target}。你会怎么做/怎么解释？"
    ]
  }
}
```

`configs/role_library.json`（示意片段，需与导演文档一致）：

```json
{
  "Host": {
    "persona": "控节奏、口语、鼓励用户说出来",
    "style_rules": ["短句", "多提问", "每轮只问一个关键问题"],
    "forbidden": ["长篇推导", "自作主张出题"]
  },
  "Economist": {
    "persona": "严谨、边界清晰、用机制链解释",
    "style_rules": ["先给一句话定义", "再给一个反例"],
    "forbidden": ["泛泛而谈", "情绪化攻击"]
  }
}
```

---

## 7. 校验与修复（Validator/Repair）：让输出永远可控

### 7.1 必做的硬校验（第一阶段）

对 ActorReply 做白名单校验：

- 字段齐全：`role_id/speech_text/user_action/interruptible_after_ms`
- `role_id` ∈ RoleSet
- `user_action.type` 与 `Plan.OutputAction` 映射一致
- `speech_text` 长度与时长估计在 `TalkBurstLimitSec` 内
- `speech_text` 禁止出现：Markdown、代码块、超长列表、URL（第一阶段尽量避免）

### 7.2 语音时长估计（工程近似即可）

第一阶段可以用近似规则（不追求完美）：

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

禁止修复：

- 替换 OutputAction（那是导演决策）
- 替换 NextBeat/NextRole（那是导演决策）

修复后应写 debug：`repaired=true`，便于后续统计模型质量。

---

## 8. 兜底策略（FallbackGenerator）：失败也要“像个产品”

### 8.1 什么时候触发兜底

- 模型超时/报错
- JSON 解析失败
- 校验失败且无法安全修复
- Realtime 连接异常导致无法播报（此时至少返回文本字幕）

### 8.2 兜底脚本的设计原则

- **永远短**：最多 2~3 句
- **永远逼输出**：必须包含 `user_action.prompt`
- **永远不跑题**：只引用 `ConceptPack.core_relation` 与当前误解/边界

示例（Recap）：

- speech：`“我们先对齐一句话：机会成本不是花掉的钱，而是你放弃的最好替代价值。你用一句话复述一下？”`
- user_action：`recap + 因为…所以…`

---

## 9. 与 gpt-realtime 的对接方式（语音原生）

在“客户端只连后端”的架构下，ActorEngine 不直接处理音频，而是输出脚本给编排器/网关：

1. 编排器拿到 `ActorReply.speech_text` 与 `voice hints`
2. 通过 Realtime Gateway 向 gpt-realtime 发出本轮播报指令（具体协议由 Gateway 封装）
3. Gateway 转发音频分片给客户端，并把 `assistant_audio_started/ended` 事件写入 Timeline

ActorEngine 需要显式提供的语音相关 hint：

- `interruptible_after_ms`：可打断点
- `voice_profile_id`（可选）：角色音色/语气（第一阶段可先固定映射）
- `speaking_rate`（可选）：慢一点/正常（fatigue 时建议慢）

> 注意：voice 的最终选择权在编排器（因为它要考虑“角色固定”“用户偏好”“疲劳”等全局信号）。

---

## 10. 与工具（Assessment/ExitTicket）的协同

### 10.1 工具注入的输入输出关系

- DirectorPlan 决定“要不要用工具/用什么类型”（通过编排器执行）
- 编排器生成 ToolPayload（题干、选项、正确映射、解释模板）
- ActorEngine 负责“宣读与引导”，而不是“发明题目”

### 10.2 语音宣读的细节（第一阶段建议）

为了语音体验，ActorEngine 在遇到 quiz 时应：

- 先一句话说明目的（例如“我们做个快速判断题”）
- 题干读一遍
- 选项用“短编号”读（A/B/C），避免长句堆叠
- 最后明确“你选 A/B/C？也可以说出来”

并在 `user_action` 中体现“需要用户回答”。

---

## 11. 可观测性（让 ActorEngine 可调参、可验收）

建议每轮记录以下信息（写入 timeline 或日志/metrics）：

- `actor.template_id`（用了哪个模板）
- `actor.generation_mode`（template / llm / realtime-text）
- `actor.validated`（是否通过校验）
- `actor.repaired`（是否修复过）
- `actor.latency_ms`
- `actor.estimated_speech_sec` vs `plan.talk_burst_limit_sec`
- `actor.user_action_type`（recap/choice/transfer…）

这些指标能直接告诉你：

- 为什么会出现“长讲压声”
- 为什么 OutputClock 失效（没有 user_action）
- 哪些模板/拍点最容易生成不合规输出

---

## 12. 测试策略（第一阶段就该有）

### 12.1 单元测试（不依赖模型）

- 给定固定 `ActorRequest`，模板路径应产生确定 `ActorReply`
- Validator 对非法输出应拒绝（缺字段/越权 role/过长）
- FallbackGenerator 对每个 OutputAction 都有覆盖（至少 Recap/Choice/Transfer）

### 12.2 契约测试（与编排器联调）

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

