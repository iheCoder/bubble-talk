<script setup>
import { nextTick, ref } from 'vue'
import HomeView from './components/HomeView.vue'
import WorldView from './components/WorldView.vue'

const stage = ref('home')
const portal = ref(null)
const portalActive = ref(false)
const selectedBubble = ref(null)
const sessionId = ref(null)

const handleEnter = async (payload) => {
  if (portalActive.value) return
  portal.value = payload
  selectedBubble.value = payload // Store the selected bubble data
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
  selectedBubble.value = null
  sessionId.value = null
}
</script>

<template>
  <div class="app-shell" :class="{ 'app-shell--warp': !!portalActive }">
    <Transition name="fade" mode="out-in">
      <HomeView v-if="stage === 'home'" @enter-world="handleEnter" :portal-active="!!portalActive" />
      <WorldView v-else :bubble="selectedBubble" :session-id="sessionId" @exit-world="handleExit" @session-created="sessionId = $event" />
    </Transition>

    <!-- Immersive Portal Transition -->
    <div
      v-if="portal && portalActive"
      class="portal-layer"
      :class="{ 'portal-layer--active': portalActive === 'animating' }"
      :style="{
        '--origin-x': `${portal.centerX}px`,
        '--origin-y': `${portal.centerY}px`,
        '--origin-size': `${portal.size}px`,
        '--portal-color': portal.glow,
      }"
    >
      <div class="portal-bubble">
        <div class="portal-bubble__inner"></div>
      </div>
      <div class="portal-flash"></div>
    </div>
  </div>
</template>

<style>
.app-shell {
  width: 100vw;
  height: 100vh;
  overflow: hidden;
  background: #000;
}

.portal-layer {
  position: fixed;
  inset: 0;
  z-index: 9999;
  pointer-events: none;
  display: flex;
  align-items: center;
  justify-content: center;
}

.portal-bubble {
  position: absolute;
  left: var(--origin-x);
  top: var(--origin-y);
  width: var(--origin-size);
  height: var(--origin-size);
  transform: translate(-50%, -50%);
  border-radius: 50%;
  background: var(--portal-color);
  transition: all 0.8s cubic-bezier(0.7, 0, 0.3, 1);
  will-change: transform, width, height, opacity;
  z-index: 1;
}

.portal-bubble__inner {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255,255,255,0.8) 0%, transparent 70%);
  opacity: 0;
  transition: opacity 0.4s ease;
}

.portal-flash {
  position: absolute;
  inset: 0;
  background: white;
  opacity: 0;
  transition: opacity 0.3s ease;
  z-index: 2;
}

.portal-layer--active .portal-bubble {
  width: 300vmax;
  height: 300vmax;
  opacity: 1;
}

.portal-layer--active .portal-bubble__inner {
  opacity: 1;
}

.portal-layer--active .portal-flash {
  animation: flash 0.8s ease forwards;
}

@keyframes flash {
  0% { opacity: 0; }
  50% { opacity: 0.8; }
  100% { opacity: 0; }
}
</style>
