<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { connectRealtime } from '../api/realtime'

const props = defineProps({
  bubble: {
    type: Object,
    default: () => ({
      title: '周末加班值不值？',
      tag: '经济',
      subtitle: '机会成本藏在时间里'
    })
  },
  sessionId: {
    type: String,
    default: null,
  },
})

const emit = defineEmits(['exit-world', 'session-created'])

// Role Configuration based on tags
const roleConfig = {
  '经济': {
    id: 'economist',
    name: '经济学家',
    tag: '机会成本',
    color: 'rgba(188, 214, 255, 0.7)',
    accent: 'rgba(140, 200, 255, 0.35)',
    avatar: 'E',
  },
  '心理': {
    id: 'psychologist',
    name: '心理咨询师',
    tag: '认知重评',
    color: 'rgba(255, 168, 209, 0.7)',
    accent: 'rgba(255, 168, 209, 0.35)',
    avatar: 'P',
  },
  '学习': {
    id: 'coach',
    name: '学习教练',
    tag: '元认知',
    color: 'rgba(124, 255, 219, 0.7)',
    accent: 'rgba(124, 255, 219, 0.35)',
    avatar: 'C',
  },
  '行为': {
    id: 'behaviorist',
    name: '行为学家',
    tag: '行为设计',
    color: 'rgba(255, 196, 110, 0.7)',
    accent: 'rgba(255, 196, 110, 0.35)',
    avatar: 'B',
  },
  '效率': {
    id: 'pm',
    name: '产品经理',
    tag: '系统思维',
    color: 'rgba(118, 245, 169, 0.7)',
    accent: 'rgba(118, 245, 169, 0.35)',
    avatar: 'PM',
  },
  '沟通': {
    id: 'mediator',
    name: '沟通专家',
    tag: '非暴力沟通',
    color: 'rgba(255, 212, 148, 0.7)',
    accent: 'rgba(255, 212, 148, 0.35)',
    avatar: 'M',
  },
  'default': {
    id: 'expert',
    name: '领域专家',
    tag: '知识向导',
    color: 'rgba(188, 214, 255, 0.7)',
    accent: 'rgba(140, 200, 255, 0.35)',
    avatar: 'X',
  }
}

const getExpertRole = (tag) => {
  return roleConfig[tag] || roleConfig['default']
}

const roles = computed(() => {
  const expert = getExpertRole(props.bubble?.tag)
  return [
    {
      id: 'host',
      name: '主持人',
      tag: '引导者',
      color: 'rgba(124, 255, 219, 0.7)',
      accent: 'rgba(124, 255, 219, 0.35)',
      avatar: 'H',
    },
    expert,
    {
      id: 'user',
      name: '你',
      tag: '学习者',
      color: 'rgba(255, 199, 140, 0.8)',
      accent: 'rgba(255, 199, 140, 0.35)',
      avatar: '你',
    },
  ]
})

const messages = ref([])
const activeRole = ref('host')
const isThinking = ref(true)
const toolState = ref('hidden')
const selectedOption = ref(null)
const toolFragment = ref(false)
const input = ref('')
const timers = []

// New state for Round Table mode
const currentSpeech = ref({
  host: null,
  expert: null, // Generic key for the second role
  user: null
})
const isMicActive = ref(true) // Default to true for RTC
const isMuted = ref(false)
const rtcClient = ref(null)

const intents = [
  '我有疑问',
  '展开一点',
  '换个例子',
  '我不信/求证',
  '我懂了，结束',
]

const expertRole = computed(() => getExpertRole(props.bubble?.tag))
const roleMap = computed(() => {
  return roles.value.reduce((acc, role) => {
    acc[role.id] = role
    return acc
  }, {})
})
const toolVisible = computed(() => toolState.value !== 'hidden')
const toolResolved = computed(() => toolState.value === 'resolved')

const ensureSession = async () => {
  // 第一阶段：把 UI 里的 bubble 映射到后端 entry_id（固定配置即可）。
  // 后续：前端改为直接展示后端 /api/bubbles 的结果。
  if (props.sessionId) return props.sessionId
  const entryId = props.bubble?.entry_id || 'econ_weekend_overtime'
  const resp = await fetch(`/api/sessions`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ entry_id: entryId }),
  })
  if (!resp.ok) throw new Error(`create session failed: ${resp.status}`)
  const data = await resp.json()
  emit('session-created', data.session_id)
  return data.session_id
}

const connect = async () => {
  try {
    errorText.value = ''
    const sessionId = await ensureSession()

    realtime = await connectRealtime({
      backendBaseUrl: '',
      sessionId,
      onRemoteStream: (stream) => {
        if (!remoteAudioEl.value) return
        remoteAudioEl.value.srcObject = stream
        remoteAudioEl.value.play().catch(() => {})
      },
      onEvent: (evt) => {
        // 这里只做最小可见性：把关键事件展示出来，便于调试。
        transcript.value.push(evt)
      },
    })
    isRealtimeConnected.value = true
    isMicActive.value = true
  } catch (err) {
    errorText.value = err?.message || String(err)
    disconnect()
  }
}

const disconnect = () => {
  isRealtimeConnected.value = false
  isMicActive.value = false
  try {
    realtime?.close()
  } catch {}
  realtime = null
  if (remoteAudioEl.value) {
    try { remoteAudioEl.value.pause() } catch {}
    remoteAudioEl.value.srcObject = null
  }
}

const pushMessage = (role, text) => {
  // Map specific expert ID to generic 'expert' key for UI positioning if needed
  // But better to use the role ID directly if we make the template dynamic

  // Clear previous speech for this role
  currentSpeech.value[role] = null

  // Set new speech with a small delay to trigger animation if needed
  setTimeout(() => {
    currentSpeech.value[role] = {
      text,
      timestamp: Date.now()
    }
  }, 10)

  // Auto-clear after some time (simulating speech duration)
  // In a real app, this would be tied to audio playback end
  const duration = Math.max(2000, text.length * 100)
  schedule(() => {
    if (currentSpeech.value[role]?.text === text) {
      currentSpeech.value[role] = null
    }
  }, duration + 1000)
}

const schedule = (fn, delay) => {
  const id = window.setTimeout(fn, delay)
  timers.push(id)
  return id
}

const playSequence = () => {
  const expert = getExpertRole(props.bubble?.tag)
  const expertId = expert.id

  // Dynamic script generation based on bubble content
  // In a real app, this would come from an API
  const steps = [
    {
      role: 'host',
      text: `欢迎来到泡泡课堂。今天我们聊聊“${props.bubble?.title || '这个话题'}”。`,
      pause: 3000,
    },
    {
      role: expertId,
      text: props.bubble?.detail || '这是一个非常值得探讨的问题，因为它触及了我们认知的盲区。',
      pause: 3000,
    },
    {
      role: 'host',
      text: '我们先来做一个直觉检验，看看大家通常是怎么想的。',
      pause: 2000,
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

    // Simulate "thinking" before speaking
    schedule(() => {
      isThinking.value = false
      pushMessage(step.role, step.text)
      if (step.after) step.after()
      schedule(() => runStep(index + 1), step.pause)
    }, 650)
  }

  runStep(0)
}

const handleSend = () => {
  if (!input.value.trim()) return
  pushMessage('user', input.value.trim())
  input.value = ''
}

const toggleMute = () => {
  isMuted.value = !isMuted.value
  if (rtcClient.value) {
    rtcClient.value.setMuted(isMuted.value)
  }
}

const handleDisconnect = () => {
  emit('exit-world')
}

const handleIntent = (intent) => {
  pushMessage('user', intent)
}

const handleOptionSelect = (option) => {
  if (toolResolved.value) return
  const expert = getExpertRole(props.bubble?.tag)

  selectedOption.value = option
  toolState.value = 'resolved'
  toolFragment.value = true
  activeRole.value = expert.id
  isThinking.value = true

  schedule(() => {
    isThinking.value = false
    pushMessage(expert.id, '很有趣的选择。这反映了我们大脑的一种典型偏好。')
    schedule(() => {
      pushMessage('host', '那么，这种偏好在其他场景下也会出现吗？')
    }, 3000)
  }, 700)

  schedule(() => {
    toolState.value = 'hidden'
  }, 5000)
}

onMounted(() => {
  playSequence()
})

// Watch for bubble changes to restart sequence if needed (though usually component is remounted)
watch(() => props.bubble, () => {
  timers.forEach((id) => window.clearTimeout(id))
  playSequence()
})

onBeforeUnmount(() => {
  timers.forEach((id) => window.clearTimeout(id))
  disconnect()
})
</script>

<template>
  <!-- 远端音频（OpenAI Realtime TTS 下行） -->
  <audio ref="remoteAudioEl" autoplay playsinline></audio>

  <div class="world-view">
    <div class="realtime-bar">
      <button class="realtime-button" @click="isRealtimeConnected ? disconnect() : connect()">
        {{ isRealtimeConnected ? '断开语音' : '连接语音（gpt-realtime / WebRTC）' }}
      </button>
      <div v-if="errorText" class="realtime-error">{{ errorText }}</div>
    </div>

    <div v-if="isRealtimeConnected" class="realtime-debug">
      <div class="realtime-debug__title">Realtime 事件（调试用）</div>
      <pre class="realtime-debug__body">{{ JSON.stringify(transcript.slice(-6), null, 2) }}</pre>
    </div>
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
    </header>

    <!-- Round Table Stage -->
    <main class="world-stage round-table">

      <!-- The Table Surface -->
      <div class="table-surface">
        <div class="table-glow"></div>
        <div class="table-grid"></div>
      </div>

      <!-- Host Position (Top Left) -->
      <div class="seat seat--host" :class="{ 'is-speaking': activeRole === 'host' && (isThinking || currentSpeech.host) }">
        <div class="avatar-container">
          <div class="avatar-halo"></div>
          <div class="avatar-circle" :style="{ '--role-color': roleMap['host'].color }">
            {{ roleMap['host'].avatar }}
          </div>
          <div class="role-label">{{ roleMap['host'].name }}</div>
        </div>
        <transition name="fade-slide" mode="out-in">
          <div v-if="currentSpeech.host" key="speech" class="speech-bubble glass-panel">
            {{ currentSpeech.host.text }}
          </div>
          <div v-else-if="activeRole === 'host' && isThinking" key="thinking" class="speech-bubble glass-panel speech-bubble--thinking">
            <span class="dot"></span><span class="dot"></span><span class="dot"></span>
          </div>
        </transition>
      </div>

      <!-- Expert Position (Top Right) -->
      <div class="seat seat--economist" :class="{ 'is-speaking': activeRole === expertRole.id && (isThinking || currentSpeech[expertRole.id]) }">
        <div class="avatar-container">
          <div class="avatar-halo"></div>
          <div class="avatar-circle" :style="{ '--role-color': expertRole.color }">
            {{ expertRole.avatar }}
          </div>
          <div class="role-label">{{ expertRole.name }}</div>
        </div>
        <transition name="fade-slide" mode="out-in">
          <div v-if="currentSpeech[expertRole.id]" key="speech" class="speech-bubble glass-panel">
            {{ currentSpeech[expertRole.id].text }}
          </div>
          <div v-else-if="activeRole === expertRole.id && isThinking" key="thinking" class="speech-bubble glass-panel speech-bubble--thinking">
            <span class="dot"></span><span class="dot"></span><span class="dot"></span>
          </div>
        </transition>
      </div>

      <!-- Center Stage (Content Board) -->
      <div class="center-stage">
        <transition name="scale-fade">
          <div v-if="toolVisible" class="content-board glass-panel holographic" :class="{ 'is-resolved': toolResolved }">
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
        </transition>
      </div>

      <!-- User Position (Bottom Center) -->
      <div class="seat seat--user" :class="{ 'is-speaking': currentSpeech.user || (!isMuted && isMicActive) }">
        <transition name="fade-slide">
          <div v-if="currentSpeech.user" class="speech-bubble glass-panel">
            {{ currentSpeech.user.text }}
          </div>
        </transition>

        <div class="user-avatar-area">
           <div class="user-avatar-wrapper">
             <div class="user-avatar-ring" :class="{ 'is-active': !isMuted && isMicActive }"></div>
             <div class="user-avatar">
               <img src="https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" alt="User Avatar" />
             </div>
             <div class="user-status-badge" :class="{ 'is-muted': isMuted }">
               {{ isMuted ? '已静音' : '聆听中' }}
             </div>
           </div>

           <div class="user-controls">
             <button class="control-btn" :class="{ 'is-active': isMuted }" @click="toggleMute" title="静音/取消静音">
               <svg v-if="!isMuted" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                 <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
                 <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
                 <line x1="12" y1="19" x2="12" y2="23"/>
                 <line x1="8" y1="23" x2="16" y2="23"/>
               </svg>
               <svg v-else width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                 <line x1="1" y1="1" x2="23" y2="23"/>
                 <path d="M9 9v3a3 3 0 0 0 5.12 2.12M15 9.34V4a3 3 0 0 0-5.94-.6"/>
                 <path d="M17 16.95A7 7 0 0 1 5 12v-2m14 0v2a7 7 0 0 1-.11 1.23"/>
                 <line x1="12" y1="19" x2="12" y2="23"/>
                 <line x1="8" y1="23" x2="16" y2="23"/>
               </svg>
             </button>
             <button class="control-btn btn-hangup" @click="handleDisconnect" title="结束通话">
               <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                 <path d="M10.68 13.31a16 16 0 0 0 3.41 2.6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7 2 2 0 0 1 1.72 2v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.42 19.42 0 0 1-3.33-2.67m-2.67-3.34a19.79 19.79 0 0 1-3.07-8.63A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91"/>
                 <line x1="23" y1="1" x2="1" y2="23"/>
               </svg>
             </button>
           </div>
        </div>
      </div>

    </main>

    <!-- Footer Controls (Intents only) -->
    <footer class="world-footer">
      <!-- Removed Intent Bar for immersion -->

      <!-- Mic Control is now part of the stage, but we keep the footer for the hidden input toggle -->
      <div class="footer-controls">
        <button class="btn-keyboard glass-panel" @click="input = ' '">
           <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
             <rect x="2" y="4" width="20" height="16" rx="2"/>
             <path d="M6 8h.01M10 8h.01M14 8h.01M18 8h.01M6 12h.01M10 12h.01M14 12h.01M18 12h.01M6 16h.01M10 16h.01M14 16h.01M18 16h.01"/>
           </svg>
        </button>
      </div>

      <!-- Hidden input for fallback -->
      <div v-if="input" class="input-overlay glass-panel">
         <input
          v-model="input"
          type="text"
          placeholder="输入你的想法..."
          @keydown.enter="handleSend"
          autoFocus
        />
        <button class="btn-close-input" @click="input = ''">✕</button>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.world-view {
  display: grid;
  grid-template-rows: auto 1fr auto;
  height: 100vh;
  background: radial-gradient(circle at 50% 50%, #1a2a4a 0%, #05070a 100%);
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
  border-bottom: none; /* Remove border for cleaner look */
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
  color: rgba(255, 255, 255, 0.9);
}

.world-tag {
  font-size: 12px;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 1px;
}

.btn-icon {
  background: rgba(255, 255, 255, 0.1);
  border: none;
  color: var(--text-primary);
  cursor: pointer;
  padding: 8px;
  border-radius: 50%;
  transition: background 0.2s;
  backdrop-filter: blur(4px);
}

/* Round Table Stage */
.round-table {
  position: relative;
  width: 100%;
  height: 100%;
  perspective: 1000px;
  display: flex;
  justify-content: center;
  align-items: center;
}

.table-surface {
  position: absolute;
  top: 55%;
  left: 50%;
  transform: translate(-50%, -50%) rotateX(60deg);
  width: 600px;
  height: 600px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.03) 0%, transparent 70%);
  border: 1px solid rgba(255, 255, 255, 0.05);
  box-shadow: 0 0 100px rgba(0, 0, 0, 0.5);
  pointer-events: none;
  z-index: 1;
}

.table-glow {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(124, 255, 219, 0.05) 0%, transparent 60%);
  animation: pulse-table 4s infinite ease-in-out;
}

.table-grid {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background-image:
    radial-gradient(rgba(255, 255, 255, 0.1) 1px, transparent 1px);
  background-size: 40px 40px;
  opacity: 0.3;
  mask-image: radial-gradient(circle, black 30%, transparent 70%);
}

@keyframes pulse-table {
  0%, 100% { opacity: 0.5; transform: scale(1); }
  50% { opacity: 0.8; transform: scale(1.05); }
}

.seat {
  position: absolute;
  display: flex;
  flex-direction: column;
  align-items: center;
  transition: all 0.5s ease;
  z-index: 10;
}

.seat--host {
  top: 10%;
  left: 15%;
  align-items: flex-start;
}

.seat--economist {
  top: 10%;
  right: 15%;
  align-items: flex-end;
}

.seat--user {
  bottom: 5%;
  left: 50%;
  transform: translateX(-50%);
  align-items: center;
  width: 100%;
}

/* Avatar Styling */
.avatar-container {
  position: relative;
  width: 80px;
  height: 80px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.avatar-circle {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.4);
  border: 2px solid var(--role-color);
  color: var(--role-color);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 24px;
  z-index: 2;
  box-shadow: 0 0 20px rgba(0,0,0,0.3);
  transition: transform 0.3s ease;
}

.seat.is-speaking .avatar-circle {
  transform: scale(1.1);
  box-shadow: 0 0 30px var(--role-color);
}

.avatar-halo {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid var(--role-color);
  opacity: 0;
  z-index: 1;
}

.seat.is-speaking .avatar-halo {
  animation: pulse-halo 2s infinite;
}

@keyframes pulse-halo {
  0% { width: 100%; height: 100%; opacity: 0.8; }
  100% { width: 160%; height: 160%; opacity: 0; }
}

.role-label {
  margin-top: 8px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.6);
  text-transform: uppercase;
  letter-spacing: 1px;
}

/* Speech Bubbles */
.speech-bubble {
  margin-top: 16px;
  padding: 16px 24px;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: #fff;
  font-size: 16px;
  line-height: 1.5;
  max-width: 280px;
  box-shadow: 0 4px 20px rgba(0,0,0,0.2);
  position: relative;
}

.seat--host .speech-bubble {
  border-top-left-radius: 4px;
  transform-origin: top left;
}

.seat--economist .speech-bubble {
  border-top-right-radius: 4px;
  transform-origin: top right;
  text-align: right;
}

.seat--user .speech-bubble {
  margin-bottom: 24px;
  margin-top: 0;
  border-bottom-left-radius: 4px;
  border-bottom-right-radius: 4px;
  background: rgba(124, 255, 219, 0.15);
  border-color: rgba(124, 255, 219, 0.3);
}

.speech-bubble--thinking {
  display: flex;
  gap: 4px;
  padding: 12px 20px;
  width: fit-content;
}

/* Center Stage */
.center-stage {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 100%;
  max-width: 400px;
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 20; /* Above table */
}

.content-board {
  width: 100%;
  background: rgba(10, 20, 40, 0.6);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 16px;
  padding: 24px;
  backdrop-filter: blur(12px);
  box-shadow: 0 20px 50px rgba(0,0,0,0.5);
  transform-style: preserve-3d;
}

.content-board.holographic {
  background: rgba(10, 20, 40, 0.4);
  border: 1px solid rgba(124, 255, 219, 0.3);
  box-shadow: 0 0 30px rgba(124, 255, 219, 0.1), inset 0 0 20px rgba(124, 255, 219, 0.05);
}

.tool-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 16px;
  color: var(--accent-color, #7cffdb);
  font-weight: 600;
  text-transform: uppercase;
  font-size: 12px;
  letter-spacing: 1px;
}

.quiz-question {
  font-size: 18px;
  font-weight: 500;
  margin-bottom: 20px;
  line-height: 1.4;
}

.quiz-options {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.quiz-option {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  padding: 16px;
  border-radius: 12px;
  color: #fff;
  text-align: left;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 15px;
}

.quiz-option:hover {
  background: rgba(255, 255, 255, 0.1);
  transform: translateX(4px);
}

.quiz-option.selected {
  background: rgba(124, 255, 219, 0.2);
  border-color: rgba(124, 255, 219, 0.5);
  color: #7cffdb;
}

/* Mic Control */
.user-avatar-area {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  margin-top: 20px;
  position: relative;
}

.user-avatar-wrapper {
  position: relative;
  width: 100px;
  height: 100px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.user-avatar {
  width: 80px;
  height: 80px;
  border-radius: 50%;
  overflow: hidden;
  border: 2px solid rgba(255, 255, 255, 0.2);
  background: #000;
  z-index: 2;
  position: relative;
}

.user-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.user-avatar-ring {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  border: 2px solid var(--accent-color, #7cffdb);
  opacity: 0;
  transform: scale(0.8);
  transition: all 0.2s;
}

.user-avatar-ring.is-active {
  opacity: 0.6;
  animation: pulse-ring 1.5s infinite;
}

@keyframes pulse-ring {
  0% { transform: scale(0.9); opacity: 0.8; }
  100% { transform: scale(1.4); opacity: 0; }
}

.user-status-badge {
  position: absolute;
  bottom: -6px;
  background: rgba(124, 255, 219, 0.2);
  border: 1px solid rgba(124, 255, 219, 0.4);
  color: #7cffdb;
  font-size: 10px;
  padding: 2px 8px;
  border-radius: 10px;
  backdrop-filter: blur(4px);
  z-index: 3;
  transition: all 0.3s;
}

.user-status-badge.is-muted {
  background: rgba(255, 100, 100, 0.2);
  border-color: rgba(255, 100, 100, 0.4);
  color: #ff8888;
}

.user-controls {
  display: flex;
  gap: 16px;
}

.control-btn {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.8);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
  backdrop-filter: blur(4px);
}

.control-btn:hover {
  background: rgba(255, 255, 255, 0.2);
  transform: translateY(-2px);
}

.control-btn.is-active {
  background: rgba(255, 100, 100, 0.2);
  color: #ff8888;
  border-color: rgba(255, 100, 100, 0.4);
}

.control-btn.btn-hangup {
  background: rgba(255, 50, 50, 0.8);
  color: white;
  border: none;
}

.control-btn.btn-hangup:hover {
  background: rgba(255, 80, 80, 1);
}

/* Footer */
.world-footer {
  padding: 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  z-index: 10;
  pointer-events: none; /* Let clicks pass through to stage */
}

.footer-controls {
  pointer-events: auto;
  position: absolute;
  bottom: 24px;
  right: 24px;
}

.intent-bar {
  display: none; /* Hide intent bar */
}

/* Transitions */
.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: all 0.3s ease;
}

.fade-slide-enter-from,
.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(10px);
}

.scale-fade-enter-active,
.scale-fade-leave-active {
  transition: all 0.4s cubic-bezier(0.175, 0.885, 0.32, 1.275);
}

.scale-fade-enter-from,
.scale-fade-leave-to {
  opacity: 0;
  transform: scale(0.9);
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
</style>
