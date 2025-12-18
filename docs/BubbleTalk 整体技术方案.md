# BubbleTalk 整体技术方案（实现导向版）

> 核心定位：把“学习”实现为一台可编排的电影剪辑机。用户看到的是自然对话与多模态素材；系统内部是一套事件驱动的会话编排、导演状态机、测评与知识追踪、推荐与泡泡生成、记忆与安全护栏。

------

## 1. 系统边界与关键设计决策

### 1.1 必须实现的产品能力（映射到技术约束）

- 对话一等公民：所有学习都围绕会话推进，不是“内容页 + 评论区”
- 可控不漂移：自由插话但主线不散，能稳定完成“转折 + 迁移”
- 学习可量化：每次会话都能更新掌握度、误解标签、复习计划
- 跨领域：经济学只是一个 domain，系统对 domain 无假设
- 多模态但不短视频化：图解/漫画/卡片是配菜，主菜是对话与练习

### 1.2 两个核心技术决策

- **双模型编排**（强烈建议）
    - Director 模型：只做路由与决策，输出结构化 DirectorPlan
    - Actor 模型：只负责台词与表现力，严格遵循 Beat 指令卡
- **状态机 + 约束优先于“让模型自由发挥”**
    - 导演输出必须过硬约束校验
    - 关键学习环节必须触发用户输出与迁移检验

------

## 2. 总体架构概览

### 2.1 组件清单

**客户端（App）**

- Bubble Home UI：泡泡云、搜索、历史、复习入口
- Session UI：对话界面、题目组件、漫画/图解卡片、分叉问题栈
- Realtime Voice：流式 ASR、TTS 播放、插话中断（barge-in）

**服务端（Core Backend）**

1. **Identity & User Profile Service**

- 显式偏好与授权信息
- 用户可编辑“偏好旋钮”（节奏、挑战度、例子密度、角色风格）

1. **Recommendation & Bubble Factory**

- 泡泡推荐（多目标）
- 泡泡生成（情境化标题、钩子、封面）
- 冷启动策略（少量显式兴趣 + 轻测）

1. **Session Orchestrator（会话编排器）**

- 会话事件循环与状态归约（reducer）
- 调用 Director、Actor、Assessment、Retrieval、Asset
- 处理 barge-in、超时、退出、恢复

1. **Director Engine（导演状态机）**

- 输入 SessionState + 用户话语 + 信号
- 输出 DirectorPlan（NextBeat/NextRole/OutputAction/栈操作等）

1. **Actor Engine（演员生成）**

- 输入 DirectorPlan + Beat 指令卡 + Concept Pack + 用户背景
- 生成下一段“可朗读台词 + 用户动作提示 + 兜底问法”

1. **Assessment Engine（测评与练习）**

- DIAGNOSE 自适应测评（2-4 题）
- 拍点内微测评（Splitter/Boundary/Transfer）
- ExitTicket（迁移题 + 一句话解释）
- 输出质量评分（rubric）

1. **Learning Model Service（知识追踪）**

- 掌握度 Mastery（0-1）
- 误解标签 MisconceptionTags
- 迁移能力 TransferScore
- 复习调度 Spaced Repetition（遗忘风险）

1. **Knowledge & Asset Layer（知识与素材层）**

- Concept Graph（概念-误解-边界-前置-迁移目标）
- Scenario Library（情境库，映射 Concept）
- Asset Store（故事、漫画、图解、卡片模板）
- 检索（概念检索、相似情境检索、引用来源）

1. **Safety & Policy Guardrails**

- 高风险领域限制（金融/医疗/法律）
- 事实性约束与“骨架生成”
- 画像推断“候选假设 + 轻确认”
- 内容过滤与拒答策略

1. **Telemetry & Session Analytics（实现学习闭环必需）**

- 会话事件流（题目结果、输出质量、插话、停顿、退出原因）
- 用于推荐与学习模型更新（不展开部署，只强调实现采集与回写）

------

## 3. 核心数据模型（跨领域通用）

### 3.1 Domain Pack（领域包）

每个领域是一组结构化资源，不是写死逻辑。

- `RoleLibrary`：可用角色模板与风格旋钮
- `BeatLibrary`：拍点卡与槽位（可复用跨领域）
- `ConceptPacks`：概念骨架（核心关系/误解/边界/迁移）
- `ScenarioPacks`：情境入口（标题钩子模板 + 关联概念）
- `QuestionTemplates`：题型模板（Splitter/Boundary/Transfer）
- `Assets`：图解/漫画/故事

### 3.2 Bubble / Entry（泡泡入口）

泡泡是“可点击的情境入口”，背后映射概念。

- `entry_id, domain, title, hook, primary_concept_id, secondary_concepts, scenario_id, difficulty_hint`
- 可选 `cover_asset_id`（插画或图解）

### 3.3 SessionState（会话状态）

导演与编排器的共享“真相源”。

- 进度：`act, beat, main_objective, objective_progress`
- 学习：`mastery_estimate, misconception_tags, transfer_readiness`
- 节奏：`output_clock_sec, tension_level, cognitive_load, pacing_mode`
- 分叉：`question_stack`
- 信号：`latency, interruptions, output_quality, affect_proxy`

### 3.4 DirectorPlan（导演计划）

结构化输出，便于硬约束校验与回放。

- `user_mind_state[]`
- `intent`（Clarify/Deepen/Branch/Meta/Off-topic）
- `next_beat, next_role, output_action`
- `talk_burst_limit_sec`
- `tension_goal, load_goal`
- `stack_action`（入栈/回收/不变）
- `notes`（导演意图）

------

## 4. 端到端互动流程（实现视角）

### 4.1 首页泡泡生成与展示

1. 客户端请求 `GetHomeBubbles(user_id, mode, context)`
2. Recommendation Service 计算多目标分数：

- InterestScore（点击、停留、追问、收藏）
- LearningNeedScore（掌握度低、误解多）
- ForgettingRiskScore（遗忘风险）
- DiversityScore（跨主题平衡）
- FatiguePenalty（疲劳抑制）

1. Bubble Factory 用 LLM 进行情境化改写（仅改标题/钩子/封面槽位）：

- 输入：ScenarioPack + 用户背景（经确认）+ 风格旋钮
- 输出：title/hook（严格长度约束）+ 可选 cover 提示词

1. 客户端渲染泡泡云（大小=推荐强度；边框/颜色=掌握度或欠账）

### 4.2 点击泡泡后会话启动

1. `CreateSession(entry_id, user_id)` 返回 session_id + initial SessionState
2. Session Orchestrator 拉取 ConceptPack/ScenarioPack/Assets
3. 进入 DIAGNOSE：

- Assessment Engine 按 ConceptPack 的误解模板出 2-4 题
- 写回：misconception_tags、mastery_estimate 初值、pacing_mode 初值

### 4.3 SEMINAR 三幕剧拍点循环

循环直到进入 EXIT：

- 客户端发送用户输入（文本或流式语音转写）
- Orchestrator 更新 signals（含 barge-in 中断事件）
- Director Engine 输出 DirectorPlan
- Guardrails 校验并修正（硬约束）
- Actor Engine 生成台词与动作提示
- 客户端展示/播放（TTS）
- OutputClock 与输出质量实时更新

### 4.4 EXIT 与 UPDATE

- ExitTicket：迁移题 + 一句话解释（强制）
- Learning Model 更新 mastery/误解/迁移/复习计划
- Recommendation 更新泡泡云与次日复习入口

------

## 5. 关键模块如何实现

## 5.1 Session Orchestrator（会话编排器）

这是系统的“剪辑台”，推荐事件驱动实现。

### 5.1.1 事件类型（最小集合）

- UserUtterance(text, ts)
- ASRPartial / ASRFinal
- TTSStarted / TTSInterrupted / TTSEnded
- QuizDelivered / QuizAnswered
- BeatStarted / BeatEnded
- SessionExitRequested

### 5.1.2 Reducer（状态归约）

实现一个纯函数 `reduce(SessionState, Event) -> SessionState`，保证：

- 可回放（回放事件流即可重建会话）
- 易做审计（导演决策可解释）
- 易插策略（硬约束校验在 plan 应用前）

------

## 5.2 Director Engine（导演状态机实现）

推荐 Hybrid Policy：硬约束 + 候选拍点打分 + LLM 路由细腻判断。

### 5.2.1 三段式决策

1. **硬约束护栏**

- output_clock >= 90s 必须选择能触发输出的 Beat
- 用户“懂了/结束”必须转 ExitTicket 或 Transfer 检验
- Branch 默认入栈并触发 BranchTeaser

1. **心智状态与意图识别**

- Heuristic：关键词、是否引入新概念、是否请求换节奏、是否输出趋于敷衍
- Router LLM：输出 `UserMindState + Intent + Confidence`
- 融合：confidence 低时以 heuristic 为准

1. **Beat Scheduler**

- 生成候选 Beat 集合（取决于 Act/当前误解/疲劳）
- 对每个 Beat 计算：
    - TruthGain（是否命中误解、是否推动输出爬梯 L1-L6）
    - FeelCurve（Fog 先 Hook/Reveal；Illusion 先 Check/Twist；Fatigue 先 MiniGame）
    - PaceFit（按 tension/load 目标）
    - Penalty（连续同类 beat、连续长讲、栈过深）
- 选最大分 Beat，输出 DirectorPlan

### 5.2.2 BeatLibrary 数据化

Beat 不写死在代码里，建议用 JSON/YAML 配置：

- 可用条件（preconditions）
- 推荐角色（roles）
- 默认输出动作（output_action）
- TalkBurstLimit 建议
- 失败分支策略（fallback）

------

## 5.3 Actor Engine（演员生成实现）

演员生成要像“执行分镜”，不是自由作文。

### 5.3.1 Prompt 构造策略

只给“当下必须的信息”，控制漂移：

- MainObjective（一句）
- 当前 MisconceptionTags（最多 2）
- 当前 Beat 卡（模板 + 槽位）
- 用户偏好旋钮（严肃度、例子密度、挑战强度）
- TalkBurstLimit
- 必须触发的 OutputAction

### 5.3.2 输出结构

建议强制结构化输出，便于客户端渲染与 TTS：

- speech_text（可朗读台词）
- user_action_prompt（明确动作：选、复述、举例、讲给谁）
- fallback_prompts（用户拒绝时的 1-2 个替代问法）
- optional_assets（建议展示的图解/漫画 asset_id 或生成提示词）

------

## 5.4 Assessment Engine（自适应测评与微测评）

题目必须“可标注误解”，否则学习追踪会漂。

### 5.4.1 题型模板（最小可用）

- Misconception Splitter：每个选项映射一个误解 tag
- Boundary Probe：适用条件/不适用条件
- Transfer Swap：换场景同结构
- Feynman Rubric：结构脚手架 + 评分点

### 5.4.2 自适应逻辑（V1）

- 初始：极简单题建立信心
- 正确但解释弱：Splitter 或 CheckBeat
- 错误但推理丰富：LensShift + 简化 Check
- 错误且命中误解：TwistBeat + Splitter
- Exit：必须 Transfer + 一句话解释

### 5.4.3 输出质量评分（实现路径）

- 规则特征：是否出现关系词、条件词、因果链结构
- LLM Rubric 判分：0-1 + 一句原因（不展示）
- 合成输出 `output_quality`，用于 mastery 更新与导演决策信号

------

## 5.5 Learning Model Service（掌握度、误解、复习）

V1 不必上复杂 BKT/IRT，但要保证可用与可迭代。

### 5.5.1 指标

- Mastery(concept_id): 0-1
- Misconception(tag): 置信度与最近出现时间
- TransferScore(concept_id): 迁移题稳定性
- ForgettingRisk: 基于间隔与稳定性估计

### 5.5.2 更新规则（V1 可实现）

- mastery += α*(quiz_correctness + output_quality + transfer) - β*(misconception_pressure)
- misconception_pressure 在 Twist/Boundary 纠正后衰减
- spaced repetition：根据 mastery 与遗忘风险生成“复习泡泡”

------

## 5.6 Recommendation & Bubble Factory（推荐与泡泡生成）

推荐必须多目标，否则会娱乐化。

### 5.6.1 推荐计算

- 候选集生成：基于 domain/历史/图谱相邻概念/复习计划
- 排序：多目标线性组合或 learning-to-rank
- 约束：多样性、疲劳抑制、欠账回补比例

### 5.6.2 泡泡文本生成（情境化）

- 输入：ScenarioPack + 用户背景（已确认）+ domain 语气风格
- 输出：短标题、短钩子（严格长度）
- 关键约束：不得引入与用户无关的敏感推断；必须可回溯到 concept_id

### 5.6.3 封面素材策略

V1 推荐：

- 默认纯文字泡泡
- 可选插画封面：由 ImageGen 生成静态插画或极轻动效提示词
- 视频不做（成本高且易偏娱乐）

------

## 5.7 Knowledge & Asset Layer（知识骨架与素材）

导演“拍大片”依赖的是稳定的知识骨架与可复用素材，而不是临场生成百科。

### 5.7.1 概念图谱（Concept Graph）

- 节点：Concept、Scenario、Misconception、Asset
- 边：prerequisite、explains、confused_with、applies_to、transfer_to
- 存储可选：图数据库或关系表 + 邻接索引

### 5.7.2 素材检索

- 会话内检索：按 concept_id 与当前 beat 拉取合适故事/漫画/图解
- 相似情境检索：向量检索 scenario embedding，辅助泡泡生成与迁移蒙太奇

### 5.7.3 骨架生成（抗幻觉）

- Actor 不直接“自由讲知识”
- Actor 只能基于 ConceptPack 的 core_relation/边界/误解/例子槽位扩写
- 对需要外部事实的内容，必须走检索并返回来源卡（可选功能）

------

## 5.8 Realtime Voice Pipeline（语音对话与插话中断）

“像电影”很大程度来自节奏与插话的即时性。

### 5.8.1 流式链路

- 客户端：麦克风音频流 -> ASR streaming -> partial transcript
- 服务端：partial 触发 “准备中断阈值判定”
- 客户端：TTS 播放中并行监听用户语音能量
- 达阈值立即 `TTSInterrupted`，提交 ASRFinal 给 orchestrator

### 5.8.2 中断后的恢复策略

- 记录被中断台词段落（用于状态信号）
- Director 下一拍点优先 Clarify/Check，快速回应后回主线或入栈分叉

------

## 5.9 Safety & Guardrails（学习场景的硬防线）

实现要点：

- 领域风控开关：domain=finance/medical/legal 时启用更强限制
- 画像推断写入：一律走“候选假设 + 轻确认”，否则不写 user memory
- 输出边界：不确定性显式化，必要时拒答或转为一般性解释
- DirectorPlan 校验：禁止产生越界动作（比如引导投资操作）

------

## 6. 两种实现路径（按复杂度）

### 路径 A：单模型快速原型

- 一个模型同时扮演导演与演员
- 输出分两段：内部 DirectorPlan + 外部台词
- 优点：开发快
- 风险：容易漂移、结构被表现力污染

### 路径 B：双模型编排（推荐）

- Director 只输出 JSON 结构
- Actor 只执行 Beat 指令卡
- 优点：稳定、可控、可调度、可扩展
- 适合产品长期演进（推荐系统、知识图谱、UGC、更多领域）

------

## 7. 你真正需要“先做出来”的最小闭环（工程切割）

按实现优先级，从最小可用到可扩展：

1. BeatLibrary + DirectorPlan schema + Guardrails（先把结构拍出来）
2. Session Orchestrator（事件循环 + reducer）
3. Assessment Engine（2-4 题 diagnose + ExitTicket）
4. Actor Engine（执行分镜，支持 3-5 个 Beat）
5. Learning Model（mastery/误解/复习）
6. Recommendation（多目标，先简单线性）
7. 泡泡情境化生成（title/hook 轻改写）
8. 语音 barge-in（最后加，但体验质变）

------

## 8. 一个简化的“全链路时序”示意（文字版）

HomeBubbles -> ClickEntry -> CreateSession
-> Diagnose(2-4Q)
-> Loop: (UserUtterance) -> DirectorPlan -> Guardrails -> ActorLine -> TTS/Render -> Signals/Reducer
-> ExitTicket(Transfer+Explain)
-> UpdateLearningModel -> UpdateRecSys -> BackHome

