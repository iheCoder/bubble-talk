<script setup>
import { computed, ref } from 'vue'

const emit = defineEmits(['exit-world'])

const roles = [
  {
    id: 'host',
    name: '主持人',
    tag: '引导者',
    color: 'rgba(124, 255, 219, 0.7)',
  },
  {
    id: 'economist',
    name: '经济学家',
    tag: '机会成本',
    color: 'rgba(188, 214, 255, 0.7)',
  },
  {
    id: 'user',
    name: '你',
    tag: '学习者',
    color: 'rgba(255, 199, 140, 0.8)',
  },
]

const messages = ref([
  {
    id: 1,
    role: 'host',
    text: '欢迎进入泡泡课堂。我们先从一个生活中的选择开始。',
  },
  {
    id: 2,
    role: 'economist',
    text: '当你加班时，你放弃的是另一段时间的潜在价值。',
  },
  {
    id: 3,
    role: 'host',
    text: '我们来做一个小检验：以下哪一个最像机会成本？',
  },
])

const activeRole = ref('economist')
const input = ref('')

const currentRole = computed(() => roles.find((role) => role.id === activeRole.value))

const sendMessage = () => {
  if (!input.value.trim()) return
  messages.value.push({
    id: Date.now(),
    role: 'user',
    text: input.value.trim(),
  })
  input.value = ''
}
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
        <div v-for="role in roles" :key="role.id" class="avatar-chip" :class="{ 'avatar-chip--active': role.id === activeRole }">
          <div class="avatar-chip__halo" :style="{ '--halo': role.color }"></div>
          <div class="avatar-chip__face">{{ role.name.slice(0, 1) }}</div>
          <div>
            <div class="avatar-chip__name">{{ role.name }}</div>
            <div class="avatar-chip__tag">{{ role.tag }}</div>
          </div>
        </div>
      </div>

      <div class="chat-stage">
        <div class="speaking-indicator" :style="{ '--halo': currentRole?.color }">
          <div class="speaking-indicator__core"></div>
          <div class="speaking-indicator__text">{{ currentRole?.name }} 正在整理想法</div>
        </div>

        <div class="chat-bubbles">
          <div
            v-for="message in messages"
            :key="message.id"
            class="chat-bubble"
            :class="`chat-bubble--${message.role}`"
          >
            {{ message.text }}
          </div>
        </div>
      </div>
    </section>

    <section class="tool-tray">
      <div class="tool-tray__card">
        <div class="tool-tray__title">QuizCard · 机会成本</div>
        <div class="tool-tray__question">哪一项最像“机会成本”？</div>
        <div class="tool-tray__options">
          <button class="option">错过的家庭晚餐</button>
          <button class="option">加班拿到的加班费</button>
          <button class="option">同事的工作压力</button>
        </div>
      </div>
    </section>

    <section class="intent-shortcuts">
      <button class="chip">我有疑问</button>
      <button class="chip">展开一点</button>
      <button class="chip">换个例子</button>
      <button class="chip">我不信/求证</button>
      <button class="chip">我懂了，结束</button>
    </section>

    <footer class="input-row">
      <input v-model="input" class="input-field" placeholder="输入你的想法或插话" />
      <button class="send-button" @click="sendMessage">发送</button>
    </footer>
  </div>
</template>
