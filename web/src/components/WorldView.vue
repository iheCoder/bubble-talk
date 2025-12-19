<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const emit = defineEmits(['exit-world'])

const roles = [
  {
    id: 'host',
    name: '主持人',
    tag: '引导者',
    color: 'rgba(124, 255, 219, 0.7)',
    accent: 'rgba(124, 255, 219, 0.35)',
    avatar: 'H',
  },
  {
    id: 'economist',
    name: '经济学家',
    tag: '机会成本',
    color: 'rgba(188, 214, 255, 0.7)',
    accent: 'rgba(140, 200, 255, 0.35)',
    avatar: 'E',
  },
  {
    id: 'user',
    name: '你',
    tag: '学习者',
    color: 'rgba(255, 199, 140, 0.8)',
    accent: 'rgba(255, 199, 140, 0.35)',
    avatar: '你',
  },
]

const messages = ref([])
const activeRole = ref('host')
const isThinking = ref(true)
const toolState = ref('hidden')
const selectedOption = ref(null)
const toolFragment = ref(false)
const input = ref('')
const timers = []

const intents = [
  '我有疑问',
  '展开一点',
  '换个例子',
  '我不信/求证',
  '我懂了，结束',
]

const currentRole = computed(() => roles.find((role) => role.id === activeRole.value))
const roleMap = computed(() => {
  return roles.reduce((acc, role) => {
    acc[role.id] = role
    return acc
  }, {})
})
const toolVisible = computed(() => toolState.value !== 'hidden')
const toolResolved = computed(() => toolState.value === 'resolved')

const pushMessage = (role, text) => {
  messages.value.push({
    id: `${Date.now()}-${Math.random().toString(16).slice(2)}`,
    role,
    text,
  })
}

const schedule = (fn, delay) => {
  const id = window.setTimeout(fn, delay)
  timers.push(id)
  return id
}

const playSequence = () => {
  const steps = [
    {
      role: 'host',
      text: '欢迎进入泡泡课堂。我们先从一个生活中的选择开始。',
      pause: 900,
    },
    {
      role: 'economist',
      text: '当你加班时，你放弃的是另一段时间的潜在价值。',
      pause: 900,
    },
    {
      role: 'host',
      text: '我们来做一个小检验：以下哪一个最像机会成本？',
      pause: 600,
      after: () => {
        toolState.value = 'show'
      },
    },
  ]

  const runStep = (index) => {
    if (index >= steps.length) return
    const step = steps[index]
    activeRole.value = step.role
    isThinking.value = true
    schedule(() => {
      isThinking.value = false
      pushMessage(step.role, step.text)
      if (step.after) step.after()
      schedule(() => runStep(index + 1), step.pause)
    }, 650)
  }

  runStep(0)
}

const sendMessage = () => {
  if (!input.value.trim()) return
  pushMessage('user', input.value.trim())
  input.value = ''
}

const sendIntent = (intent) => {
  pushMessage('user', intent)
}

const selectOption = (option) => {
  if (toolResolved.value) return
  selectedOption.value = option
  toolState.value = 'resolved'
  toolFragment.value = true
  activeRole.value = 'economist'
  isThinking.value = true
  schedule(() => {
    isThinking.value = false
    pushMessage('economist', '是的，错过的家庭晚餐是你真正放弃的价值。')
    schedule(() => {
      pushMessage('host', '很棒。接下来我们把它和“沉没成本”做对比。')
    }, 900)
  }, 700)

  schedule(() => {
    toolState.value = 'hidden'
  }, 2600)
}

onMounted(() => {
  playSequence()
})

onBeforeUnmount(() => {
  timers.forEach((id) => window.clearTimeout(id))
})
</script>

<template>
  <div class="world-view">
    <!-- Header Layer -->
    <header class="world-header glass-panel">
      <div class="world-header__left">
        <button class="btn-icon" @click="emit('exit-world')">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M19 12H5M12 19l-7-7 7-7" />
          </svg>
        </button>
        <div class="world-title">
          <h1>周末加班值不值？</h1>
          <span class="world-tag">经济学 · 机会成本</span>
        </div>
      </div>
      <div class="world-header__right">
        <div class="avatar-stack">
          <div v-for="role in roles" :key="role.id" class="avatar-mini" :style="{ '--role-color': role.color }">
            {{ role.avatar }}
          </div>
        </div>
      </div>
    </header>

    <!-- Stage Layer (Dialogue) -->
    <main class="world-stage">
      <div class="chat-stream">
        <div
          v-for="msg in messages"
          :key="msg.id"
          class="chat-row"
          :class="{ 'chat-row--user': msg.role === 'user' }"
        >
          <div class="chat-avatar" :style="{ '--role-color': roleMap[msg.role].color }">
            {{ roleMap[msg.role].avatar }}
          </div>
          <div class="chat-bubble glass-panel">
            <div class="chat-name">{{ roleMap[msg.role].name }}</div>
            <div class="chat-text">{{ msg.text }}</div>
          </div>
        </div>

        <!-- Thinking Indicator -->
        <div v-if="isThinking" class="chat-row chat-row--thinking">
          <div class="chat-avatar" :style="{ '--role-color': currentRole.color }">
            <div class="speaking-halo"></div>
            {{ currentRole.avatar }}
          </div>
          <div class="chat-bubble glass-panel chat-bubble--thinking">
            <span class="dot"></span><span class="dot"></span><span class="dot"></span>
          </div>
        </div>
      </div>
    </main>

    <!-- Tool Tray Layer -->
    <div class="tool-tray" :class="{ 'tool-tray--visible': toolVisible, 'tool-tray--resolved': toolResolved }">
      <div class="tool-card glass-panel">
        <div class="tool-header">
          <span class="tool-icon">⚡️</span>
          <span class="tool-title">快速检验</span>
        </div>
        <div class="quiz-content">
          <div class="quiz-question">以下哪一个最像机会成本？</div>
          <div class="quiz-options">
            <button
              v-for="(opt, idx) in ['看电影花的50元', '看电影花掉的2小时', '看电影时买的爆米花']"
              :key="idx"
              class="quiz-option"
              :class="{ 'selected': selectedOption === idx }"
              @click="handleOptionSelect(idx)"
            >
              {{ opt }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Control Layer -->
    <footer class="world-footer glass-panel">
      <div class="intent-bar">
        <button v-for="intent in intents" :key="intent" class="intent-chip" @click="handleIntent(intent)">
          {{ intent }}
        </button>
      </div>
      <div class="input-area">
        <input
          v-model="input"
          type="text"
          placeholder="输入你的想法..."
          @keydown.enter="handleSend"
        />
        <button class="btn-send" @click="handleSend">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z" />
          </svg>
        </button>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.world-view {
  display: grid;
  grid-template-rows: auto 1fr auto;
  height: 100vh;
  background: radial-gradient(circle at 50% 100%, #1a2a4a 0%, #05070a 100%);
  position: relative;
  overflow: hidden;
}

/* Header */
.world-header {
  padding: 16px 24px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  z-index: 10;
  border-bottom: 1px solid var(--glass-border);
}

.world-header__left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.world-title h1 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
  line-height: 1.2;
}

.world-tag {
  font-size: 12px;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 1px;
}

.btn-icon {
  background: none;
  border: none;
  color: var(--text-primary);
  cursor: pointer;
  padding: 8px;
  border-radius: 50%;
  transition: background 0.2s;
}

.btn-icon:hover {
  background: rgba(255, 255, 255, 0.1);
}

.avatar-mini {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  border: 1px solid var(--role-color);
  color: var(--role-color);
  margin-left: -8px;
  backdrop-filter: blur(4px);
}

.avatar-stack {
  display: flex;
  padding-left: 8px;
}

/* Stage */
.world-stage {
  overflow-y: auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 24px;
  scroll-behavior: smooth;
}

.chat-row {
  display: flex;
  gap: 16px;
  max-width: 80%;
  opacity: 0;
  animation: slideIn 0.4s forwards;
}

.chat-row--user {
  align-self: flex-end;
  flex-direction: row-reverse;
}

@keyframes slideIn {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
}

.chat-avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--role-color);
  color: var(--role-color);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  position: relative;
  flex-shrink: 0;
}

.speaking-halo {
  position: absolute;
  inset: -4px;
  border-radius: 50%;
  border: 2px solid var(--role-color);
  opacity: 0;
  animation: pulse 2s infinite;
}

@keyframes pulse {
  0% { transform: scale(1); opacity: 0.5; }
  100% { transform: scale(1.5); opacity: 0; }
}

.chat-bubble {
  padding: 16px 20px;
  border-radius: 4px 20px 20px 20px;
  position: relative;
}

.chat-row--user .chat-bubble {
  border-radius: 20px 4px 20px 20px;
  background: rgba(255, 255, 255, 0.1);
  border-color: rgba(255, 255, 255, 0.2);
}

.chat-name {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 4px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.chat-text {
  font-size: 15px;
  line-height: 1.6;
}

.chat-bubble--thinking {
  display: flex;
  gap: 4px;
  align-items: center;
  padding: 12px 20px;
}

.dot {
  width: 4px;
  height: 4px;
  background: currentColor;
  border-radius: 50%;
  animation: bounce 1.4s infinite ease-in-out both;
}

.dot:nth-child(1) { animation-delay: -0.32s; }
.dot:nth-child(2) { animation-delay: -0.16s; }

@keyframes bounce {
  0%, 80%, 100% { transform: scale(0); }
  40% { transform: scale(1); }
}

/* Tool Tray */
.tool-tray {
  position: fixed;
  bottom: 100px;
  left: 50%;
  transform: translateX(-50%) translateY(100px);
  width: 90%;
  max-width: 600px;
  opacity: 0;
  pointer-events: none;
  transition: all 0.5s cubic-bezier(0.19, 1, 0.22, 1);
  z-index: 20;
}

.tool-tray--visible {
  transform: translateX(-50%) translateY(0);
  opacity: 1;
  pointer-events: auto;
}

.tool-card {
  padding: 24px;
  border-radius: 24px;
  background: rgba(10, 20, 35, 0.85);
  border: 1px solid var(--glow-mint);
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.5);
}

.tool-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  color: var(--glow-mint);
  font-weight: 600;
  text-transform: uppercase;
  font-size: 12px;
  letter-spacing: 1px;
}

.quiz-question {
  font-size: 16px;
  font-weight: 500;
  margin-bottom: 16px;
}

.quiz-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.quiz-option {
  padding: 12px 16px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  color: var(--text-primary);
  text-align: left;
  cursor: pointer;
  transition: all 0.2s;
}

.quiz-option:hover {
  background: rgba(255, 255, 255, 0.1);
}

.quiz-option.selected {
  background: rgba(124, 255, 219, 0.15);
  border-color: var(--glow-mint);
  color: var(--glow-mint);
}

/* Footer */
.world-footer {
  padding: 16px 24px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  border-top: 1px solid var(--glass-border);
  z-index: 10;
}

.intent-bar {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 4px;
  scrollbar-width: none;
}

.intent-chip {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: var(--text-secondary);
  padding: 6px 12px;
  border-radius: 16px;
  font-size: 12px;
  white-space: nowrap;
  cursor: pointer;
  transition: all 0.2s;
}

.intent-chip:hover {
  background: rgba(255, 255, 255, 0.1);
  color: var(--text-primary);
}

.input-area {
  display: flex;
  gap: 12px;
}

input {
  flex: 1;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 24px;
  padding: 12px 20px;
  color: white;
  font-family: inherit;
  outline: none;
  transition: border-color 0.2s;
}

input:focus {
  border-color: var(--glow-ice);
}

.btn-send {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: var(--text-primary);
  color: var(--bg-space-0);
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: transform 0.2s;
}

.btn-send:hover {
  transform: scale(1.05);
}
</style>
