<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { BubbleTalkGateway, AudioPlayer } from '../api/gateway.js'
import hostAvatar from '../assets/host.png'
import { getExpertRole } from './worldview/roles.js'
import WorldHeader from './worldview/WorldHeader.vue'
import WorldStage from './worldview/WorldStage.vue'
import WorldFooter from './worldview/WorldFooter.vue'

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
      avatarImage: hostAvatar,
      voice: 'marin',
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

const isMicActive = ref(false) // 初始为 false，连接后才启用
// 产品预期：默认进入即“聆听中”，否则用户会误以为系统无响应。
const isMuted = ref(false)
const isAssistantSpeaking = ref(false)
const hasSentIntro = ref(false)
// tts_completed 表示“后端不再发送音频”，但前端播放队列可能还未播完；
// 用 onDrain 做最终收口，避免说话特效提前结束。
const ttsDrainArmed = ref(false)

// WebSocket Gateway 相关
const gateway = ref(null)
const audioPlayer = ref(null)
const isConnecting = ref(false)
const isConnected = ref(false)
const connectionError = ref('')

// 转写（仅用于控制流；不做 UI 回显）
const partialTranscript = ref('')

// Quiz相关状态
const currentQuiz = ref(null) // 当前显示的quiz
const quizHistory = ref([]) // 答题历史

// 诊断题目（废弃，现在由LLM动态生成）
const diagnose = ref({
  questions: []
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
    audioPlayer.value.onDrain = () => {
      if (!ttsDrainArmed.value) return
      // 以实际音频播放队列耗尽作为“说话结束”，避免 tts_completed(服务端发送完成) 早于前端播放完成。
      isAssistantSpeaking.value = false
      ttsDrainArmed.value = false
    }

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
      ttsDrainArmed.value = false
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
      console.log('[WorldView] ✅ 用户说:', text)
    }

    // TTS 开始 - AI 开始说话
    gateway.value.onTTSStarted = (metadata) => {
      isAssistantSpeaking.value = true
      isThinking.value = false
      ttsDrainArmed.value = false

      // 关键：从 metadata 中获取角色
      if (metadata?.role) {
        activeRole.value = metadata.role
        console.log('[WorldView] 🎭 角色说话:', metadata.role)
      } else {
        // 兜底：使用专家角色
        activeRole.value = expertRole.value.id
        console.warn('[WorldView] ⚠️  metadata 中没有 role，使用默认:', expertRole.value.id)
      }

      console.log('[WorldView] 🔊 AI 开始说话, activeRole=', activeRole.value)
    }

    // TTS 完成 - 等待前端音频播放队列耗尽再清除说话状态
    gateway.value.onTTSCompleted = (metadata) => {
      ttsDrainArmed.value = true
      const ctx = audioPlayer.value?.audioContext
      const nextStartTime = audioPlayer.value?.nextStartTime
      if (ctx && typeof nextStartTime === 'number') {
        const remainingSec = Math.max(0, nextStartTime - ctx.currentTime)
        if (remainingSec <= 0.05) {
          isAssistantSpeaking.value = false
          ttsDrainArmed.value = false
        }
      }
      // 也清除 activeRole，避免特效一直显示
      // activeRole.value = null  // 可选：是否要清除
      console.log('[WorldView] ✅ AI 说话完成, role:', metadata?.role)
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

      activeRole.value = role
      isThinking.value = false
      void beat
    }

    // 接收Quiz - 显示选择题
    gateway.value.onQuizShow = (quizData) => {
      console.log('[WorldView] 📝 收到选择题:', quizData)
      currentQuiz.value = {
        quiz_id: quizData.quiz_id,
        question: quizData.question,
        options: quizData.options,
        context: quizData.context
      }
      toolState.value = 'visible' // 显示工具面板
      selectedOption.value = null // 清空之前的选择
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
  ttsDrainArmed.value = false
  if (gateway.value) {
    gateway.value.stopRecording()
    gateway.value.disconnect()
    gateway.value = null
  }
  isConnected.value = false
}

const schedule = (fn, delay) => {
  const id = window.setTimeout(fn, delay)
  timers.push(id)
  return id
}

const handleSend = () => {
  if (!input.value.trim()) return
  // Send to gateway but don't display user speech
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

// 处理用户答题
const handleQuizAnswer = (optionIndex) => {
  if (!currentQuiz.value || !gateway.value) return

  const answer = currentQuiz.value.options[optionIndex]
  selectedOption.value = optionIndex

  console.log('[WorldView] 用户选择:', answer)

  // 发送答题结果到后端
  gateway.value.sendQuizAnswer(currentQuiz.value.quiz_id, answer)

  // 保存到历史
  quizHistory.value.push({
    quiz_id: currentQuiz.value.quiz_id,
    question: currentQuiz.value.question,
    answer: answer,
    timestamp: new Date()
  })

  // 标记为已完成
  toolState.value = 'resolved'

  // 3秒后隐藏
  setTimeout(() => {
    toolState.value = 'hidden'
    currentQuiz.value = null
    selectedOption.value = null
  }, 3000)
}

onMounted(() => {
  connect()
})

// Watch for bubble changes to restart sequence if needed (though usually component is remounted)
watch(() => props.bubble, () => {
  timers.forEach((id) => window.clearTimeout(id))
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
    <WorldHeader
      :title="props.bubble?.title || '今日话题'"
      :tag="props.bubble?.tag || '主题'"
      :expert-tag="expertRole.tag"
      :is-connected="isRealtimeConnected"
      @exit="emit('exit-world')"
      @toggle-connection="isRealtimeConnected ? disconnect() : connect()"
    />

    <div v-if="connectionError" class="error-toast">
      {{ connectionError }}
      <button @click="connectionError = ''">✕</button>
    </div>

    <WorldStage
      :role-map="roleMap"
      :expert-role="expertRole"
      :active-role="activeRole"
      :is-assistant-speaking="isAssistantSpeaking"
      :is-thinking="isThinking"
      :current-quiz="currentQuiz"
      :diagnose="diagnose"
      :tool-visible="toolVisible"
      :tool-resolved="toolResolved"
      :selected-option="selectedOption"
      :is-muted="isMuted"
      :is-mic-active="isMicActive"
      @toggle-mute="toggleMute"
      @hangup="handleDisconnect"
      @answer-quiz="handleQuizAnswer"
    />

    <WorldFooter v-model:input="input" @send="handleSend" />
  </div>
</template>

<style scoped>
:global(:root) {
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

:global(.glass-panel) {
  background: rgba(255, 255, 255, 0.05);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(255, 255, 255, 0.1);
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
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
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
