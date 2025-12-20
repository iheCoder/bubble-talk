<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { BubbleTalkGateway, AudioPlayer } from '../api/gateway.js'

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

const activeRole = ref('host')
const isThinking = ref(false)
const toolState = ref('hidden')
const selectedOption = ref(null)
const input = ref('')
const timers = []

// New state for Round Table mode
const currentSpeech = ref({
  host: null,
  expert: null,
  user: null
})
const isMicActive = ref(false) // 初始为 false，连接后才启用
const isMuted = ref(true)
const isAssistantSpeaking = ref(false)
const hasSentIntro = ref(false)

// WebSocket Gateway 相关
const gateway = ref(null)
const audioPlayer = ref(null)
const isConnecting = ref(false)
const isConnected = ref(false)
const connectionError = ref('')

// 转写和调试
const transcript = ref([])
const partialTranscript = ref('')

// 诊断题目
const diagnose = ref({
  questions: [
    {
      prompt: '周末加班800元，你会选择哪个？',
      options: [
        'A. 赚钱，毕竟800块不少',
        'B. 休息，健康更重要',
        'C. 看情况，要考虑很多因素'
      ]
    }
  ]
})

const isRealtimeConnected = computed(() => isConnected.value)

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
  const resp = await fetch(`http://localhost:8080/api/sessions`, {
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
  if (isConnecting.value || isConnected.value) return

  try {
    isConnecting.value = true
    connectionError.value = ''

    // 确保有 session
    const sessionId = await ensureSession()
    console.log('[WorldView] Session ID:', sessionId)

    // 创建 Gateway
    gateway.value = new BubbleTalkGateway(sessionId)
    audioPlayer.value = new AudioPlayer()

    // 设置事件回调
    gateway.value.onConnected = async () => {
      isConnected.value = true
      isConnecting.value = false
      console.log('[WorldView] ✅ Gateway 连接成功')

      // 自动开始录音（如果没有静音）
      if (!isMuted.value) {
        try {
          await gateway.value.startRecording()
          isMicActive.value = true
          console.log('[WorldView] 🎤 自动开始录音')
        } catch (err) {
          console.error('[WorldView] ❌ 录音失败:', err)
        }
      }

      // 进入 World 后，导演先主动开场
      requestDirectorIntro()
    }

    gateway.value.onDisconnected = () => {
      isConnected.value = false
      isAssistantSpeaking.value = false
      console.log('[WorldView] Gateway 断开')
    }

    // ASR 实时转写
    gateway.value.onASRPartial = (text) => {
      partialTranscript.value = text
      console.log('[WorldView] 部分转写:', text)
    }

    // ASR 最终转写 - 用户说完了
    gateway.value.onASRFinal = (text) => {
      partialTranscript.value = ''
      pushMessage('user', text)
      transcript.value.push({ type: 'user', text, time: new Date() })
      console.log('[WorldView] ✅ 用户说:', text)
    }

    // TTS 开始 - AI 开始说话
    gateway.value.onTTSStarted = () => {
      isAssistantSpeaking.value = true
      isThinking.value = false
      console.log('[WorldView] 🔊 AI 开始说话')
    }

    // TTS 完成
    gateway.value.onTTSCompleted = () => {
      isAssistantSpeaking.value = false
      console.log('[WorldView] ✅ AI 说话完成')
    }

    // 接收音频数据并播放
    gateway.value.onAudioData = async (blob) => {
      console.log('[WorldView] 🎵 收到音频:', blob.size, 'bytes')
      await audioPlayer.value.playAudioBlob(blob)
    }

    // 接收助手文本 - 显示哪个角色在说话
    gateway.value.onAssistantText = (text, metadata) => {
      const role = metadata?.role || expertRole.value.id
      const beat = metadata?.beat

      console.log('[WorldView] 💬 AI 说话:', role, text)

      // 显示对话气泡
      activeRole.value = role
      isThinking.value = false
      pushMessage(role, text)

      // 记录到转写历史
      transcript.value.push({
        type: 'assistant',
        role,
        text,
        beat,
        time: new Date()
      })
    }

    // 错误处理
    gateway.value.onError = (error) => {
      connectionError.value = error.message
      console.error('[WorldView] ❌ 错误:', error)
      isConnecting.value = false
    }

    // 连接 WebSocket
    await gateway.value.connect()

  } catch (err) {
    connectionError.value = err.message
    isConnecting.value = false
    isConnected.value = false
    console.error('[WorldView] ❌ 连接失败:', err)
  }
}

const disconnect = () => {
  isMicActive.value = false
  isAssistantSpeaking.value = false
  isThinking.value = false
  hasSentIntro.value = false
  if (gateway.value) {
    gateway.value.stopRecording()
    gateway.value.disconnect()
    gateway.value = null
  }
  isConnected.value = false
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

const handleSend = () => {
  if (!input.value.trim()) return
  pushMessage('user', input.value.trim())
  input.value = ''
}

const requestDirectorIntro = () => {
  if (!gateway.value || hasSentIntro.value) return
  hasSentIntro.value = true
  isThinking.value = true
  gateway.value.sendWorldEntered({
    bubble_title: props.bubble?.title || '',
    bubble_tag: props.bubble?.tag || '',
  })
}

const toggleMute = () => {
  isMuted.value = !isMuted.value
  if (gateway.value) {
    if (isMuted.value) {
      gateway.value.stopRecording()
      isMicActive.value = false
    } else if (isMicActive.value) {
      gateway.value.startRecording()
    } else if (isConnected.value) {
      gateway.value.startRecording()
      isMicActive.value = true
    }
  }
}

const handleDisconnect = () => {
  emit('exit-world')
}


onMounted(() => {
  connect()
})

// Watch for bubble changes to restart sequence if needed (though usually component is remounted)
watch(() => props.bubble, () => {
  timers.forEach((id) => window.clearTimeout(id))
  currentSpeech.value = { host: null, expert: null, user: null }
  hasSentIntro.value = false
  if (isConnected.value) {
    requestDirectorIntro()
  }
})

onBeforeUnmount(() => {
  timers.forEach((id) => window.clearTimeout(id))
  disconnect()
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
          <h1>{{ props.bubble?.title || '今日话题' }}</h1>
          <span class="world-tag">{{ props.bubble?.tag || '主题' }} · {{ expertRole.tag }}</span>
        </div>
      </div>

      <div class="world-header__right">
        <button
          class="realtime-button"
          :class="{ 'is-connected': isRealtimeConnected }"
          @click="isRealtimeConnected ? disconnect() : connect()"
        >
          <span class="status-dot"></span>
          {{ isRealtimeConnected ? '语音已连接' : '连接语音' }}
        </button>
      </div>
    </header>

    <div v-if="isRealtimeConnected && transcript.length > 0" class="realtime-debug">
      <div class="realtime-debug__title">调试日志</div>
      <div class="realtime-debug__content">
        <div v-for="(evt, i) in transcript.slice(-3)" :key="i" class="debug-item">
          {{ evt.type }}
        </div>
      </div>
    </div>

    <div v-if="connectionError" class="error-toast">
      {{ connectionError }}
      <button @click="connectionError = ''">✕</button>
    </div>

    <!-- Round Table Stage -->
    <main class="world-stage round-table">
      <div class="table-orbit">
        <!-- The Table Surface -->
        <div class="table-surface">
          <div class="table-glow"></div>
          <div class="table-grid"></div>
          <div class="table-rim"></div>
          <div class="table-core"></div>
        </div>

        <!-- Center Stage (Content Board) -->
        <div class="center-stage">
          <transition name="scale-fade">
            <div v-if="toolVisible" class="content-board glass-panel holographic" :class="{ 'is-resolved': toolResolved }">
              <div class="tool-header">
                <span class="tool-icon">⚡️</span>
                <span class="tool-title">快速检验</span>
              </div>
              <div class="quiz-content" v-if="diagnose && diagnose.questions && diagnose.questions.length > 0">
                <div class="quiz-question">{{ diagnose.questions[0].prompt }}</div>
                <div class="quiz-options">
                  <button
                    v-for="(opt, idx) in diagnose.questions[0].options"
                    :key="idx"
                    class="quiz-option"
                    :class="{ 'selected': selectedOption === idx }"
                  >
                    {{ opt }}
                  </button>
                </div>
              </div>
              <div class="quiz-content" v-else>
                <div class="quiz-question">加载题目中...</div>
              </div>
            </div>
          </transition>
        </div>

        <!-- Host Position (Top Left) -->
      <div class="seat seat--host" :class="{ 'is-speaking': activeRole === 'host' && (isAssistantSpeaking || currentSpeech.host) }">
        <div class="avatar-container" :style="{ '--role-color': roleMap['host'].color }">
          <div class="avatar-halo"></div>
          <div class="avatar-ripple"></div>
          <div class="avatar-ripple avatar-ripple--delay"></div>
          <div class="avatar-circle">
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
      <div class="seat seat--economist" :class="{ 'is-speaking': activeRole === expertRole.id && (isAssistantSpeaking || currentSpeech[expertRole.id]) }">
        <div class="avatar-container" :style="{ '--role-color': expertRole.color }">
          <div class="avatar-halo"></div>
          <div class="avatar-ripple"></div>
          <div class="avatar-ripple avatar-ripple--delay"></div>
          <div class="avatar-circle">
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
             <div class="user-avatar-wave" :class="{ 'is-active': !!currentSpeech.user }"></div>
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
:root {
  --role-color: #fff;
  --accent-color: #7cffdb;
}

.world-view {
  display: grid;
  grid-template-rows: auto 1fr auto;
  height: 100vh;
  background: radial-gradient(circle at 50% 40%, #162f53 0%, #05070a 70%);
  position: relative;
  overflow: hidden;
}

.world-view::before {
  content: '';
  position: absolute;
  inset: -10% -20%;
  background:
    radial-gradient(circle at 20% 20%, rgba(124, 255, 219, 0.12), transparent 45%),
    radial-gradient(circle at 80% 30%, rgba(255, 190, 120, 0.1), transparent 50%),
    radial-gradient(circle at 50% 80%, rgba(140, 200, 255, 0.12), transparent 55%);
  filter: blur(30px);
  opacity: 0.8;
  z-index: 0;
}

.world-view::after {
  content: '';
  position: absolute;
  inset: 0;
  background-image:
    radial-gradient(rgba(255, 255, 255, 0.06) 1px, transparent 1px);
  background-size: 120px 120px;
  opacity: 0.08;
  z-index: 0;
}

.world-view > * {
  position: relative;
  z-index: 1;
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
  overflow: hidden; /* Prevent scrollbars if animations go out */
}

.table-orbit {
  position: absolute;
  left: 50%;
  top: 56%;
  transform: translate(-50%, -50%);
  width: 90vmin;
  height: 90vmin;
  max-width: 920px;
  max-height: 920px;
}

.table-orbit::before {
  content: '';
  position: absolute;
  inset: 4%;
  border-radius: 50%;
  border: 1px solid rgba(124, 255, 219, 0.08);
  box-shadow: 0 0 40px rgba(124, 255, 219, 0.08);
  opacity: 0.6;
}

.table-orbit::after {
  content: '';
  position: absolute;
  inset: -6%;
  border-radius: 50%;
  border: 1px dashed rgba(255, 255, 255, 0.1);
  opacity: 0.35;
  animation: orbit-spin 40s linear infinite;
}

@keyframes orbit-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.table-surface {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%) rotateX(58deg);
  width: 68vmin; /* Responsive size */
  height: 68vmin;
  max-width: 680px;
  max-height: 680px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.02) 0%, transparent 70%);
  border: 1px solid rgba(255, 255, 255, 0.05);
  box-shadow:
    0 0 50px rgba(0, 0, 0, 0.5),
    inset 0 0 100px rgba(0, 0, 0, 0.8);
  pointer-events: none;
  z-index: 1;
  transform-style: preserve-3d;
}

.table-glow {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(124, 255, 219, 0.03) 0%, transparent 60%);
  animation: pulse-table 6s infinite ease-in-out;
}

.table-grid {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background-image:
    radial-gradient(rgba(255, 255, 255, 0.15) 1px, transparent 1px);
  background-size: 8% 8%; /* Relative grid size */
  opacity: 0.2;
  mask-image: radial-gradient(circle, black 40%, transparent 80%);
}

.table-rim {
  position: absolute;
  inset: 4%;
  border-radius: 50%;
  border: 2px solid rgba(124, 255, 219, 0.15);
  box-shadow:
    0 0 30px rgba(124, 255, 219, 0.2),
    inset 0 0 20px rgba(124, 255, 219, 0.15);
  opacity: 0.8;
}

.table-core {
  position: absolute;
  inset: 28%;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(124, 255, 219, 0.1), transparent 70%);
  box-shadow: inset 0 0 25px rgba(124, 255, 219, 0.2);
}

@keyframes pulse-table {
  0%, 100% { opacity: 0.3; transform: scale(1); }
  50% { opacity: 0.6; transform: scale(1.02); }
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
  top: 18%;
  left: 18%;
  transform: translate(-50%, -50%);
  align-items: flex-start;
}

.seat--economist {
  top: 18%;
  left: 82%;
  transform: translate(-50%, -50%);
  align-items: flex-end;
}

.seat--user {
  top: 92%;
  left: 50%;
  transform: translate(-50%, -50%);
  align-items: center;
  width: auto;
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

.avatar-ripple {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid var(--role-color);
  opacity: 0;
  transform: translate(-50%, -50%) scale(0.85);
  z-index: 0;
  filter: drop-shadow(0 0 12px rgba(255, 255, 255, 0.12));
}

.avatar-ripple--delay {
  animation-delay: 0.6s;
}

.seat.is-speaking .avatar-ripple {
  animation: ripple-wave 1.8s infinite ease-out;
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

@keyframes ripple-wave {
  0% { transform: translate(-50%, -50%) scale(0.85); opacity: 0.7; }
  70% { opacity: 0.25; }
  100% { transform: translate(-50%, -50%) scale(1.65); opacity: 0; }
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
  transform: translate(-50%, -50%) rotateX(18deg);
  width: 42vmin;
  height: 42vmin;
  max-width: 460px;
  max-height: 460px;
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 20;
  pointer-events: auto; /* Allow interaction */
  filter: drop-shadow(0 20px 40px rgba(0, 0, 0, 0.45));
}

.center-stage::before {
  content: '';
  position: absolute;
  inset: 6%;
  border-radius: 50%;
  border: 1px solid rgba(124, 255, 219, 0.2);
  box-shadow: inset 0 0 20px rgba(124, 255, 219, 0.2);
  opacity: 0.6;
  pointer-events: none;
}

.content-board {
  width: 100%;
  height: 100%;
  max-width: 460px;
  max-height: 460px;
  background: rgba(10, 20, 40, 0.2);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 50%;
  padding: 32px;
  backdrop-filter: blur(8px);
  box-shadow:
    0 0 40px rgba(0, 0, 0, 0.35),
    inset 0 0 40px rgba(124, 255, 219, 0.08);
  transform-style: preserve-3d;
  transition: all 0.5s cubic-bezier(0.23, 1, 0.32, 1);
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  text-align: center;
}

.content-board.holographic {
  background:
    radial-gradient(circle at center, rgba(124, 255, 219, 0.18) 0%, transparent 70%),
    radial-gradient(circle at 30% 20%, rgba(255, 255, 255, 0.08), transparent 60%);
  border: 1px solid rgba(124, 255, 219, 0.2);
  box-shadow:
    0 0 60px rgba(124, 255, 219, 0.12),
    inset 0 0 50px rgba(124, 255, 219, 0.08);
}

.tool-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
  color: var(--accent-color, #7cffdb);
  font-weight: 600;
  text-transform: uppercase;
  font-size: 11px;
  letter-spacing: 1px;
  opacity: 0.8;
}

.quiz-question {
  font-size: 17px;
  font-weight: 500;
  margin-bottom: 18px;
  line-height: 1.4;
  color: rgba(255, 255, 255, 0.9);
  max-width: 80%;
}

.quiz-options {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
  align-items: center;
}

.quiz-option {
  width: 78%;
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.06), rgba(124, 255, 219, 0.06));
  border: 1px solid rgba(255, 255, 255, 0.12);
  padding: 12px 18px;
  border-radius: 999px;
  color: rgba(255, 255, 255, 0.8);
  text-align: center;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 14px;
}

.quiz-option:hover {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.12), rgba(124, 255, 219, 0.14));
  transform: translateY(-2px);
}

.quiz-option.selected {
  background: rgba(124, 255, 219, 0.15);
  border-color: rgba(124, 255, 219, 0.4);
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

.user-avatar-wave {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid rgba(255, 199, 140, 0.7);
  opacity: 0;
  transform: translate(-50%, -50%) scale(0.9);
  transition: opacity 0.2s;
  z-index: 1;
}

.user-avatar-wave.is-active {
  opacity: 0.6;
  animation: ripple-wave 1.7s infinite ease-out;
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

@media (max-width: 900px) {
  .table-orbit {
    width: 96vmin;
    height: 96vmin;
    top: 58%;
  }

  .table-surface {
    width: 72vmin;
    height: 72vmin;
  }

  .center-stage {
    width: 48vmin;
    height: 48vmin;
  }

  .seat--host {
    top: 16%;
    left: 14%;
  }

  .seat--economist {
    top: 16%;
    left: 86%;
  }

  .seat--user {
    top: 94%;
  }
}

@media (max-width: 600px) {
  .center-stage {
    width: 54vmin;
    height: 54vmin;
    transform: translate(-50%, -50%) rotateX(12deg);
  }

  .content-board {
    padding: 24px;
  }

  .quiz-option {
    width: 86%;
  }
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

/* Utility Classes */
.glass-panel {
  background: rgba(255, 255, 255, 0.05);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(255, 255, 255, 0.1);
}

.btn-keyboard {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255, 255, 255, 0.8);
  cursor: pointer;
  transition: all 0.2s;
}

.btn-keyboard:hover {
  background: rgba(255, 255, 255, 0.15);
  transform: translateY(-2px);
}

.input-overlay {
  position: fixed;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  width: 90%;
  max-width: 600px;
  padding: 8px;
  border-radius: 12px;
  display: flex;
  gap: 8px;
  z-index: 100;
  box-shadow: 0 10px 30px rgba(0,0,0,0.5);
}

.input-overlay input {
  flex: 1;
  background: transparent;
  border: none;
  color: white;
  font-size: 16px;
  padding: 8px 12px;
  outline: none;
}

.btn-close-input {
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  padding: 0 12px;
  font-size: 18px;
}

.btn-close-input:hover {
  color: white;
}

/* Realtime Controls */
.realtime-button {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.7);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  backdrop-filter: blur(4px);
}

.realtime-button:hover {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  transform: translateY(-1px);
}

.realtime-button.is-connected {
  background: rgba(124, 255, 219, 0.15);
  border-color: rgba(124, 255, 219, 0.3);
  color: #7cffdb;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.realtime-button.is-connected .status-dot {
  box-shadow: 0 0 8px currentColor;
}

.realtime-debug {
  position: absolute;
  top: 80px;
  left: 24px;
  width: 200px;
  background: rgba(0, 0, 0, 0.5);
  border-radius: 8px;
  padding: 12px;
  font-size: 10px;
  color: rgba(255, 255, 255, 0.6);
  pointer-events: none;
  z-index: 5;
}

.realtime-debug__title {
  font-weight: 600;
  margin-bottom: 4px;
  text-transform: uppercase;
  opacity: 0.5;
}

.debug-item {
  margin-bottom: 2px;
  font-family: monospace;
}

.error-toast {
  position: absolute;
  top: 80px;
  left: 50%;
  transform: translateX(-50%);
  background: rgba(255, 80, 80, 0.9);
  color: white;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  z-index: 100;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
}

.error-toast button {
  background: transparent;
  border: none;
  color: white;
  cursor: pointer;
  opacity: 0.8;
  padding: 0;
}
</style>
