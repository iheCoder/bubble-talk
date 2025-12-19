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
    <header class="world-header">
      <div>
        <div class="world-kicker">Bubble Session</div>
        <h2 class="world-title">周末加班值不值？</h2>
        <p class="world-subtitle">机会成本 · 价值选择 · 生活实验</p>
      </div>
      <button class="ghost-button" @click="emit('exit-world')">返回宇宙</button>
    </header>

    <section class="world-stage">
      <div class="avatar-row">
        <div
          v-for="role in roles"
          :key="role.id"
          class="avatar-chip"
          :class="{ 'avatar-chip--active': role.id === activeRole }"
        >
          <div class="avatar-chip__halo" :style="{ '--halo': role.color }"></div>
          <div class="avatar-chip__face" :style="{ '--accent': role.accent }">{{ role.avatar }}</div>
          <div>
            <div class="avatar-chip__name">{{ role.name }}</div>
            <div class="avatar-chip__tag">{{ role.tag }}</div>
          </div>
        </div>
      </div>

      <div class="chat-stage">
        <div class="speaking-indicator" :style="{ '--halo': currentRole?.color }">
          <div class="speaking-indicator__core"></div>
          <div class="speaking-indicator__text">
            {{ currentRole?.name }} {{ isThinking ? '正在整理想法' : '正在回应' }}
          </div>
        </div>

        <TransitionGroup name="bubble" tag="div" class="chat-bubbles">
          <div
            v-for="message in messages"
            :key="message.id"
            class="chat-bubble"
            :class="`chat-bubble--${message.role}`"
          >
            <div class="chat-bubble__meta" :style="{ '--accent': roleMap[message.role]?.accent }">
              <span class="chat-bubble__role">{{ roleMap[message.role]?.name }}</span>
              <span class="chat-bubble__tag">{{ roleMap[message.role]?.tag }}</span>
            </div>
            <div class="chat-bubble__text">{{ message.text }}</div>
          </div>
        </TransitionGroup>
      </div>
    </section>

    <section
      v-show="toolVisible"
      class="tool-tray"
      :class="{ 'tool-tray--active': toolState === 'show', 'tool-tray--exit': toolState === 'resolved' }"
    >
      <div class="tool-tray__card">
        <div class="tool-tray__title">QuizCard · 机会成本</div>
        <div class="tool-tray__question">哪一项最像“机会成本”？</div>
        <div class="tool-tray__options">
          <button
            class="option"
            :class="{ 'option--selected': selectedOption === '错过的家庭晚餐' }"
            @click="selectOption('错过的家庭晚餐')"
          >
            错过的家庭晚餐
          </button>
          <button
            class="option"
            :class="{ 'option--selected': selectedOption === '加班拿到的加班费' }"
            @click="selectOption('加班拿到的加班费')"
          >
            加班拿到的加班费
          </button>
          <button
            class="option"
            :class="{ 'option--selected': selectedOption === '同事的工作压力' }"
            @click="selectOption('同事的工作压力')"
          >
            同事的工作压力
          </button>
        </div>
      </div>
    </section>

    <section v-if="toolFragment" class="tool-fragment">
      <div class="tool-fragment__chip">已收藏 · 机会成本</div>
    </section>

    <section class="intent-shortcuts">
      <button v-for="intent in intents" :key="intent" class="chip" @click="sendIntent(intent)">
        {{ intent }}
      </button>
    </section>

    <footer class="input-row">
      <input v-model="input" class="input-field" placeholder="输入你的想法或插话" />
      <button class="send-button" @click="sendMessage">发送</button>
    </footer>
  </div>
</template>
