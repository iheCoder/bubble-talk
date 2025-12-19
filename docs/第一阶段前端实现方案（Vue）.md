# BubbleTalk 第一阶段前端实现方案（Vue）

> 语音优先 × 工具托盘 × 对话一等公民
> 
> 以语音为默认交互；不做传统聊天列表，而是“圆桌舞台态”：发言人、字幕、工具卡片托盘（ToolTray）按剧情入场与退场，保持节奏与沉浸。会话事件以统一模型驱动（asr_final/quiz_answer/intent_hint/barge_in/exit_requested）。MVP-1 中语音轨与字幕由 OpenAI Realtime 直连（WebRTC），事件镜像与测评通过后端 HTTP 维持“事实源”与可验收。

---

## 0. 范围与目标（MVP-1）

- Home（探索）
  - 泡泡宇宙：展示固定泡泡（GET /api/bubbles），轻物理漂浮、悬停聚焦、点击进入。
  - 过滤星盘：领域/难度/偏好半透明面板（可简化为占位）。
- World（对话课堂）
  - 圆桌三角位：主持人/专家/用户三节点；发言高亮、呼吸光环、波纹指示。
  - 语音链路：浏览器→OpenAI Realtime（WebRTC）；远端音轨自动播放；字幕实时。
  - 工具托盘：DIAGNOSE 2 题、EXIT 1 题+解释，像“道具”入场；选项反馈与解释承接。
  - 意图快捷：底部气泡（intent_hint）：“我有疑问/举个例子/换种说法/我不信/我懂了结束”。
  - 调试面板：最近 3 条事件/plan 摘要（便于联调）。
  - 退出：缩放退场回宇宙。
- 系统约束（对话一等公民）
  - 90 秒输出钟：必须触发一次用户输出（复述/选择/举例/迁移）。
  - 结束语义：用户说“结束/我懂了”→强制 Exit Ticket。
  - 插话中断（barge-in）可见且可裁决。

---

## 1. 信息架构与路由

- 路由
  - `/`（Home）：泡泡宇宙，点击进入会话。
  - `/world/:sessionId`（World）：对话课堂；若无 `sessionId`，创建后 `router.replace`。
- 组件层级（现有/新增）
  - Home：`BubbleUniverse.vue`（或现有组合）、`BubbleNode.vue`、`StarDustLayer.vue`、`FilterConstellationPanel.vue`
  - World：`WorldView.vue`（现存，宿主）
    - WorldStage（新增，舞台容器）
      - AvatarChip（主持/专家/用户头像+SpeakingHalo）
      - CenterStage/ContentBoard（字幕/摘要/富媒体承载）
      - ToolTray（托盘容器）→ QuizCard、DiagramCard、RubricPill（MVP 用 QuizCard）
      - IntentShortcuts（底部快捷意图）
      - RealtimeButton/MicControl（连接/静音）
      - DebugPanel（调试浮层）

---

## 2. 前后端契约（HTTP + Realtime）

- HTTP
  - `GET /api/bubbles` → Bubble[]（entry_id/domain/title/hook/primary_concept_id）
  - `POST /api/sessions` → { session_id, state(SessionState), diagnose(2 题) }
  - `POST /api/sessions/{id}/realtime/token` → { model, voice, ephemeral_key, expires_at, instructions }
  - `POST /api/sessions/{id}/events`（调试/镜像）→ EventResponse { assistant, debug.director_plan }
- Realtime（WebRTC，浏览器直连 OpenAI）
  - 音轨：上行用户麦克风；下行 TTS，autoplay，barge-in 时本地 duck/暂停。
  - DataChannel：
    - 上行：`session.update`（注入 instructions）、`response.create`（开场白/提示）
    - 下行：模型事件（转写/响应片段/错误）。前端收到“最终转写”后，镜像为 `asr_final` 通过 HTTP `/events` 上报（形成事实源）。
- 事件模型（前端 → 后端 `/events`）
  - `asr_final`：{ type, text }
  - `quiz_answer`：{ type, question_id, answer }
  - `intent_hint`：{ type, text }
  - `barge_in`：{ type }
  - `exit_requested`：{ type }
  - `user_message`（兜底文本）：{ type, text }
- ToolTray 触发规则（前端）
  - 会话创建返回 `diagnose` → 立即显示托盘 2 题。
  - 收到 `debug.director_plan` 的 `next_beat=Check` 或 `output_action∈{Recap,Transfer,Exit}` → 提升托盘显示优先级（若在关键讲话中，延时入场）。
  - `exit_requested` → 强制显示 Exit Ticket，完成前禁离场。

---

## 3. 组件设计（关键属性/事件）

- WorldStage（新增）
  - props：sessionId、roles、connection、mic、activeTool、subtitleNow
  - emits：`quiz_answer(questionId, answer)`、`intent_hint(text)`、`exit_requested()`、`barge_in()`
- AvatarChip
  - props：role、isSpeaking、isThinking、accent
- SpeakingHalo
  - 根据 isSpeaking 渲染能量波纹；MVP 用“发言态切换”驱动。
- CenterStage/ContentBoard
  - 展示字幕/摘要；ToolTray 出现时上移让出空间。
- ToolTray
  - props：visible、tool（{ type, payload }）
  - emits：`quiz_answer`、`resolve`
  - 动效：自底部上升 12–16px；卡片滑入、回弹落定；完成后折叠/淡出。
- QuizCard
  - props：question（id/prompt/options）、selected
  - emits：`select(option)`
  - 反馈：选中即高亮；0.3–0.6s 后触发解释由角色口播。
- IntentShortcuts
  - 固定列表：我有疑问/举个例子/换种说法/我不信/我懂了结束 → `intent_hint`。
- RealtimeButton/MicControl
  - 连接/断开、静音切换；连接成功后默认麦克风开。
- DebugPanel
  - 最近 3 条事件与 `director_plan` 摘要。

---

## 4. 状态管理（最小可行）

- 技术：保持 JS + Composition API（可选 Pinia 后续引入）。
- Stores（建议 provide/inject）
  - sessionStore：
    - sessionId、state（SessionState 快照）、diagnose
    - connection：{ pc, dc, localStream, remoteAudioEl?、isConnected }
    - mic：{ muted, active }
    - transcripts：{ partial, final[] }
    - debugPlan：{ intent, next_beat, output_action, ... }
  - uiStore：
    - activeRole、isThinking
    - toolTrayState：hidden/show/resolved
    - activeTool：null | { type: 'quiz'|'diagram'|'rubric', payload }
    - subtitleNow
    - toasts：错误提示队列
- 同步来源
  - Realtime 下行 → transcripts/subtitles、speaking markers、错误
  - `/events` 回包 → assistant 文本（调试）、`debug.director_plan`（托盘触发参考）

---

## 5. 语音链路与 Realtime 对接（对齐 `web/src/api/realtime.js`）

- 会话建立：若无 `sessionId` → `POST /api/sessions { entry_id }`，保存 `session_id/state/diagnose`。
- Realtime token：`POST /api/sessions/{id}/realtime/token` 获取 `ephemeral_key/model/instructions`。
- WebRTC：`getUserMedia(audio)`、`RTCPeerConnection.addTrack`、`ontrack → remoteAudioEl.srcObject`。
- DataChannel：`oai-events`
  - `onopen`：发送 `session.update(instructions)`、`response.create(欢迎语)`。
  - `onmessage`：解析转写事件；对“最终转写”镜像为 `asr_final` 发往 `/events`。
- barge-in：用户开始说话（或长按麦克）→ 本地先 `remoteAudioEl.pause()` 或降低音量；再 `POST /events { type: 'barge_in' }`。

---

## 6. 交互动效要点（验收项）

- 发言节奏：发言人高亮、SpeakingHalo 波纹；非发言人熄灭；用户发言时底部麦克风区声波反馈。
- ToolTray 入场：过渡台词→托盘上升→卡片滑入→选择反馈→角色解释→托盘收束。
- 插话中断：barge-in 时舞台短暂停镜 0.2s（CenterStage 轻微停顿/缩放），用户光环点亮；随后恢复。
- 退场：Exit Ticket 完成后舞台收束、镜头拉远。

---

## 7. 错误与边界

- 麦克风权限拒绝：提示并切到 `user_message` 文本输入兜底路径；提供重试授权。
- Realtime token 失败：错误 toast + 可重试；日志最小化。
- SDP/网络失败：断开并展示重连；保持 `sessionId`；恢复字幕与 UI。
- 自动播放限制：连接后引导点击一次播放按钮解锁。
- 双击/重复答题：QuizCard 一次性选择；重复点击无效；提交节流。
- 远端覆盖用户：barge-in 本地先行降音/暂停远端；保障打断权。

---

## 8. 构建/部署与本地开发

- 本地
  - 后端：设置 `OPENAI_API_KEY`（可选 `OPENAI_REALTIME_MODEL/VOICE`）；启动 Go 服务。
  - 前端：`npm i` → `npm run dev`（Vite）；CORS 已放行 `http://localhost:5173`。
  - 打开 Home → 进入 World → 点击“连接语音”建立链路。
- 构建
  - `npm run build` 产物可由任意静态服务器托管；建议与后端同域反代；生产收紧 CORS。

---

## 9. 数据与最小类型（JSDoc）

```js
/** @typedef {{entry_id:string, domain:string, title:string, hook:string, primary_concept_id:string}} Bubble */
/** @typedef {{id:string, prompt:string, options:string[]}} QuizQuestion */
/** @typedef {{session_id:string, state:any, diagnose:{questions:QuizQuestion[]}}} CreateSessionResponse */
/** @typedef {{type:string, text?:string, question_id?:string, answer?:string, client_ts?:string}} Event */
/** @typedef {{user_mind_state:string[], intent:string, next_beat:string, next_role:string, output_action:string}} DirectorPlan */
```

---

## 10. 测试要点（可验收清单）

- 语音链路：首次连接成功率、上下行、autoplay、静音、barge-in 实效。
- 事件镜像：final 字幕→/events 上报；返回 director_plan 与 UI 提示一致；90s 输出钟触发托盘。
- 工具托盘：DIAGNOSE/EXIT 呈现、选中态、解释承接；防抖；完成后状态恢复。
- 插话：远端音轨即时 duck/暂停；UI 暂停/恢复；续播平滑。
- 错误：麦克风拒绝、token/SDP 失败、网络中断重连；文字兜底有效。
- 兼容：macOS Chrome/Safari（移动端列为后续）。

---

## 11. 未来扩展

- 会话总线：客户端只连后端（WebSocket 或 WebRTC-to-backend），统一时间线。
- Timeline 拉流与回放 UI：对齐“事实源 = 事件流”。
- Pinia 与类型增强；Sentry/可观测性。
- 设备选择/回声消除/VAD/语速控制。
- 工具卡片族（对比表/流程图模板）与“学习碎片”收藏区。
- 移动端与 PWA。

---

## 12. 现有文件与落点

- 前端
  - `web/src/components/WorldView.vue`：圆桌舞台、语音连接、调试浮层（作为 WorldStage 宿主）。
  - `web/src/api/realtime.js`：OpenAI Realtime WebRTC 封装；会话初始化与初始发言。
  - `web/src/components/BubbleNode.vue`、`StarDustLayer.vue`、`FilterConstellationPanel.vue`：宇宙层。
- 后端
  - `server/internal/api/server.go`：HTTP 契约、realtime/token、events 回包 DebugPayload。
  - `server/internal/model/types.go`：Bubble/SessionState/Event/DirectorPlan/DiagnoseSet。
  - `server/configs/bubbles.json`：固定泡泡配置。
