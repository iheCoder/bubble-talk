<script setup>
import { nextTick, ref } from 'vue'
import HomeView from './components/HomeView.vue'
import WorldView from './components/WorldView.vue'

const stage = ref('home')
const portal = ref(null)
const portalActive = ref(false)

const handleEnter = async (payload) => {
  if (portalActive.value) return
  portal.value = payload
  portalActive.value = true
  await nextTick()
  requestAnimationFrame(() => {
    portalActive.value = 'animating'
    window.setTimeout(() => {
      stage.value = 'world'
      portalActive.value = false
      portal.value = null
    }, 820)
  })
}

const handleExit = () => {
  stage.value = 'home'
}
</script>

<template>
  <div class="app-shell">
    <HomeView v-if="stage === 'home'" @enter-world="handleEnter" :portal-active="!!portalActive" />
    <WorldView v-else @exit-world="handleExit" />

    <div
      v-if="portal && portalActive"
      class="transition-portal"
      :class="{ 'transition-portal--animate': portalActive === 'animating' }"
      :style="{
        '--start-x': `${portal.centerX}px`,
        '--start-y': `${portal.centerY}px`,
        '--start-size': `${portal.size}px`,
        '--glow': portal.glow,
      }"
    >
      <div class="transition-portal__core">
        <div class="transition-portal__title">{{ portal.title }}</div>
        <div class="transition-portal__subtitle">{{ portal.subtitle }}</div>
      </div>
    </div>
  </div>
</template>
