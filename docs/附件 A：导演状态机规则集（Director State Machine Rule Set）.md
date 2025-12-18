# 附件 A：导演状态机规则集（Director State Machine Rule Set）V1

> 目的：让对话“自由但不散”，始终围绕一个明确学习目标推进，并在合适的时刻触发用户输出（检索/解释/迁移），避免“陪聊化”和“听懂幻觉”。

------

## 0. 术语与设计原则

### 0.1 三条硬原则（不可违反）

1. **单会话单主目标**：每次会话只确保吃透 1 个主概念/关系（MainObjective），其他问题入栈回收。
2. **定期用户输出**：每 90 秒必须触发一次用户输出动作（说/写/选/举例），否则强制插入检查。
3. **可控分叉**：允许用户随时插话，但插话必须路由到明确类别（Clarify/Deepen/Branch/Meta/Off-topic），并给出处理策略。

### 0.2 导演（Director）是什么

- 导演是一个“隐形调度器”，不直接对用户发声（或只用极短提示）。
- 导演控制：
    - 当前阶段（Stage）
    - 目标（Objective）
    - 节奏（Pacing）
    - 出题时机（QuizTriggers）
    - 分叉问题栈（QuestionStack）
    - 退出条件（ExitTicket）

------

## 1. 状态机总体结构

### 1.1 会话状态（SessionState）

**生命周期**：ENTER → DIAGNOSE → SEMINAR → EXIT → UPDATE

- **ENTER**：进入泡泡，初始化会话目标与角色阵容
- **DIAGNOSE**：2–4 题自适应测评，产出掌握度与误解标签
- **SEMINAR**：对话研讨课（核心）
- **EXIT**：离场仪式（迁移题 + 一句话解释）
- **UPDATE**：写回用户模型，更新泡泡推荐

### 1.2 研讨课子状态（SeminarStage）

研讨课固定 5 段（可循环一次，但不建议超过 2 轮）：

1. **HOOK**：故事/现象引入（情境化）
2. **MODEL**：核心关系建模（短讲）
3. **CHECK**：用户复述/判断（必有）
4. **CHALLENGE**：反例、边界、常见误解对抗
5. **TRANSFER**：新场景迁移应用（必有）

**阶段顺序默认：HOOK → MODEL → CHECK → CHALLENGE → TRANSFER → EXIT**

------

## 2. 导演维护的数据结构（必须字段）

### 2.1 目标与进度

- `MainObjective`：本节要吃透的核心关系（单句）
- `SubGoals`：{clarify_points[], misconception_targets[], boundary_points[], transfer_targets[]}
- `Stage`：当前阶段（HOOK/MODEL/...）
- `StageProgress`：当前阶段完成度（0–100）
- `LoopCount`：研讨课循环次数（默认 1，最多 2）

### 2.2 用户状态快照（会话内）

- `MasteryEstimate`：对 MainObjective 的掌握度（0–1）
- `MisconceptionTags`：本次命中的误解标签集合
- `EngagementSignals`：{turns, interruptions, response_latency, verbosity, affect_proxy}
- `OutputClock`：距离上次“有效输出动作”的秒数计时器

### 2.3 分叉管理

- `QuestionStack`：用户分叉问题栈（LIFO 或按优先级）
    - item: {question, type_hint, urgency, timestamp, stage_at_ask}
- `ParkingLot`：暂存材料/例子（随时可回收）

### 2.4 节奏控制

- `PacingMode`：{FAST, NORMAL, DEEP}
- `TalkBurstLimit`：单次角色连续讲述时长上限（秒）
- `InterventionLevel`：导演介入强度（低/中/高）

------

## 3. 插话路由规则（最关键）

### 3.1 插话分类器（分类输出）

对每个用户输入/插话，导演必须输出一个类别：

- **Clarify**：澄清当前内容（术语/例子/因果）
- **Deepen**：加深当前内容（更深入机制/推导）
- **Branch**：开启新概念/新主题（明显超出 MainObjective）
- **Meta**：节奏/难度/角色偏好/“我学得对吗”
- **Off-topic**：与学习目标无关（闲聊/跑题）

> 注：分类可用规则 + 轻量模型/LLM。V1 可先用启发式：是否包含当前 Objective 关键词、是否问“为什么/如何推导”、是否指向新领域名词等。

### 3.2 路由策略（对不同分类怎么处理）

**Clarify（立刻答，短）**

- 动作：即时解释（≤ 25 秒或 ≤ 4 句）
- 然后：回到当前 Stage 的下一步
- 若 Clarify 出现 ≥ 2 次且同一点：标记为 `clarify_point_hard`，在 CHECK 强制复述

**Deepen（立刻答，但必须“锚回主线”）**

- 动作：给一个更深层机制或更严格定义（≤ 40 秒）
- 然后：用一句“锚点句”拉回 MainObjective
    - 例：“这解释了为什么我们说 X 的本质是…”
- 若 Deepen 连续发生：导演切换到 `PacingMode=DEEP` 并减少素材外扩

**Branch（入栈 + 给选择）**

- 动作：把问题压入 `QuestionStack`，并给用户一个二选一提示（导演可短句提示）：
    - 选项 A：现在顺着分支聊 2 分钟（开启小循环，LoopCount+1）
    - 选项 B：先走完主线，结束前回到这个问题（默认）
- 默认策略：除非用户强烈要求或分支与当前误解强相关，否则走 B

**Meta（调整策略）**

- 若用户说“太简单/太难/太快/太慢”：调整 `PacingMode`、题目难度、讲述比例
- 若用户说“换角色/换风格”：切换角色阵容（见第 6 节）
- 若用户问“我学会了吗”：不口头安慰，直接触发 ExitTicket 的一部分（迁移题或复述）

**Off-topic（轻回正轨）**

- 动作：一句回应 + 立刻回到最近的 `SubGoal`
- 若连续 2 次 Off-topic：触发短休止提示或建议退出/稍后继续

------

## 4. 阶段推进规则（Stage Transition Rules）

### 4.1 HOOK → MODEL

触发条件（满足其一）：

- 用户表达了“我想知道为什么/怎么解释”
- 用户对情境做出初步判断或提问
- HOOK 进行超过 60–90 秒

导演动作：

- 压缩情境为一句“研究问题”
- 宣告 MainObjective（内部，不必对用户显式）

### 4.2 MODEL → CHECK（强制）

触发条件：

- 角色完成一次短讲（TalkBurstLimit 内）
- 或 OutputClock ≥ 60 秒（用户一直在听）

导演动作：触发 **CHECK 动作**（见第 5 节）

### 4.3 CHECK → CHALLENGE

触发条件：

- 用户复述/判断达到最低合格（见 5.3）
- 或用户明显卡住但愿意继续（给最小提示后再 CHECK 一次）

### 4.4 CHALLENGE → TRANSFER

触发条件：

- 至少击中 1 个 MisconceptionTag 并纠正
- 或完成 1 个反例边界讨论
- CHALLENGE 超过 2 分钟仍无收敛则强制转移到 TRANSFER（用迁移题检验）

### 4.5 TRANSFER → EXIT

触发条件：

- 完成 1 次迁移应用（用户能把概念用到新场景）
- 或用户要求退出但未迁移：也必须先做最小 ExitTicket

------

## 5. “用户输出动作”规则（Anti-Illusion Engine）

### 5.1 有效输出动作（Valid Outputs）

任意一种即可重置 OutputClock：

- **One-sentence recap**：一句话复述核心关系（限字/限时）
- **Forced choice**：做一道判断/选择题（偏概念结构而非记忆）
- **Example generation**：给一个生活/工作例子并说明为什么符合
- **Boundary test**：给反例或说明何时不适用
- **Teach-back**：把它讲给“同学角色”听（20–30 秒）

### 5.2 触发阈值

- 若 `OutputClock >= 90s`：必须插入一次输出动作
- 若用户连续 2 次只点“嗯/好/懂了”：立即插入输出动作（防“听懂幻觉”）
- 若用户说“我懂了/我会了/可以结束了”：直接跳到 ExitTicket（迁移题 + 一句话解释）

### 5.3 合格判定（V1 简化）

- 复述必须包含：主语/关键关系词/结果（例如“因为…所以…”）
- 判断题必须给出“为什么”（一句话也行）
- 例子必须指明：对应概念的哪个结构点

不合格处理：

- 给“最小提示”（不超过 1 句）
- 再让用户重试一次
- 仍不行：降低难度或切换例子，再次 CHECK

------

## 6. 角色调度规则（Role Orchestration）

### 6.1 默认角色组合（可配置）

- **经济学**：专家（经济学家）+ 主持（主持人）+ 挑战（质疑者）
- **编程**：同伴（两位同学）+ 专家（资深工程师/Reviewer）
- **历史/人文**：故事（记者/旁白）+ 专家（学者）+ 挑战（怀疑者）
- **心理学/自我提升**：教练（Socratic）+ 同伴（陪练）

### 6.2 角色切换触发

- 用户显式要求：“换成更严肃/更轻松/更多例子/更像访谈”
- EngagementSignals 显示：
    - 用户输出低：增加同伴角色提问、减少专家讲述
    - 用户追问深：增加挑战角色，进入 DEEP 模式
    - 用户焦虑/卡顿：增加主持角色做节奏缓冲，降低挑战强度

### 6.3 TalkBurstLimit（防讲座化）

- FAST：每次讲述 ≤ 20 秒
- NORMAL：≤ 35 秒
- DEEP：≤ 45 秒（仍需 CHECK）

------

## 7. 出题策略（Quiz Injection Rules）

### 7.1 题目注入时机

- DIAGNOSE 阶段：固定 2–4 题
- SEMINAR 阶段：
    - 当命中 MisconceptionTag 时，立刻出 1 道“误解对抗题”
    - 当用户说“懂了”时，出 1 道“迁移题”
    - 当对话漂移或过长无输出时，出 1 道“结构判断题”
- EXIT 阶段：固定 ExitTicket

### 7.2 题目类型模板（V1 三种够用）

1. **Misconception Splitter**：每个选项对应一个误解
2. **Boundary Probe**：何时适用/不适用
3. **Transfer Swap**：换场景但同结构

------

## 8. 分叉问题栈回收规则（Branch Recovery）

### 8.1 回收时机

- TRANSFER 完成后
- 或 EXIT 前（若用户时间足够）
- 或用户主动点“回到我刚才的问题”

### 8.2 回收策略

- 每次只回收 1 个分叉问题（避免再次发散）
- 回收过程必须绑定回 MainObjective 或明确新 Objective
    - 若分叉问题与 MainObjective 强相关：作为 Deepen
    - 若弱相关：提议生成新泡泡（下次学习入口）

------

## 9. 退出条件与离场仪式（Exit Ticket Rules）

### 9.1 退出触发

- 用户主动退出
- 会话达到时长上限（默认 12 分钟）
- 导演判断完成度达标且用户输出稳定

### 9.2 Exit Ticket（强制两步）

1. **迁移题 1 道**（换场景、换表述、换干扰逻辑）
2. **一句话自我解释**（限时 20 秒/限字 25）

### 9.3 退出后总结模板（最多 3 行）

- 核心关系（你刚学会的“镜头”）
- 你最易犯的误解（MisconceptionTag）
- 下一步建议（一个泡泡 + 理由）

------

## 10. 失败与降级策略（Fail-safe）

### 10.1 用户沉默/不愿输出

- 允许“选择题模式”（用点选替代口述）
- 或让同伴角色先回答，用户只需“挑哪里不对”

### 10.2 用户频繁跑题

- 提升 InterventionLevel
- 更频繁插入 CHECK
- 提议生成“分支泡泡”并回主线

### 10.3 生成内容不确定/争议大

- 角色应明确标注不确定性
- 引导用户查看来源卡（后续模块）
- 避免给出具体高风险决策建议

------

## 11. V1 交付标准（验收条款）

- 插话分类准确率：人工评估 ≥ 80%（5 类中）
- OutputClock 规则有效：95% 会话内每 90 秒至少一次有效输出
- 主线不散：80% 会话能完成 HOOK→MODEL→CHECK→CHALLENGE→TRANSFER
- Exit Ticket 覆盖率：≥ 90%（除非用户强制关闭）
- 用户主观感受：对话“像研讨课而不是陪聊”满意度 ≥ 4/5（小规模内测）

------

## 12. 示例：经济学泡泡“周末加班值不值？”的一次状态流（摘要）

ENTER → DIAGNOSE（2 题定位把机会成本误解为“花出去的钱”）
SEMINAR:

- HOOK：讲 offer/周末时间分配故事（60s）
- MODEL：专家短讲“机会成本=放弃的最好替代方案价值”（30s）
- CHECK：用户一句话复述（不合格→给最小提示→再复述）
- CHALLENGE：主持人抛反例“沉没成本不是机会成本”；挑战者出误解对抗题
- TRANSFER：换到“团队资源分配/政策选择”场景让用户应用
  EXIT：迁移题 + 一句话解释
  UPDATE：掌握度上升，误解标签减少，首页泡泡更新