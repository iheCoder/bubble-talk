<script setup>
import { computed, ref } from 'vue'

const props = defineProps({
  bubble: {
    type: Object,
    required: true,
  },
})

const emit = defineEmits(['select', 'drag-start', 'drag-end', 'hover-start', 'hover-end'])
const nodeRef = ref(null)

const style = computed(() => ({
  transform: `translate3d(${props.bubble.x - props.bubble.radius}px, ${props.bubble.y - props.bubble.radius}px, 0)`,
  width: `${props.bubble.radius * 2}px`,
  height: `${props.bubble.radius * 2}px`,
  '--bubble-glow': props.bubble.glow,
}))

const handleClick = () => {
  const rect = nodeRef.value?.getBoundingClientRect()
  if (!rect) return
  emit('select', {
    id: props.bubble.id,
    title: props.bubble.title,
    subtitle: props.bubble.subtitle,
    glow: props.bubble.glow,
    centerX: rect.left + rect.width / 2,
    centerY: rect.top + rect.height / 2,
    size: rect.width,
  })
}
</script>

<template>
  <div
    ref="nodeRef"
    class="bubble-node"
    :class="{ 'bubble-node--hover': bubble.hover, 'bubble-node--dim': bubble.dim }"
    :style="style"
    @pointerdown.stop="emit('drag-start', bubble, $event)"
    @pointerup.stop="emit('drag-end')"
    @pointerenter="emit('hover-start', bubble)"
    @pointerleave="emit('hover-end', bubble)"
    @click.stop="handleClick"
  >
    <!-- Outer Glow Halo -->
    <div class="bubble-node__halo"></div>

    <!-- Main Glass Sphere -->
    <div class="bubble-node__glass">
      <!-- Internal Fog/Texture -->
      <div class="bubble-node__fog"></div>

      <!-- Content Layer -->
      <div class="bubble-node__content">
        <div class="bubble-node__tag">{{ bubble.tag }}</div>
        <div class="bubble-node__title">{{ bubble.title }}</div>

        <!-- Revealed on Hover -->
        <div class="bubble-node__reveal">
          <div class="bubble-node__subtitle">{{ bubble.subtitle }}</div>
          <div class="bubble-node__divider"></div>
          <div class="bubble-node__keywords">
            <span v-for="keyword in bubble.keywords" :key="keyword" class="bubble-node__keyword">#{{ keyword }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.bubble-node {
  position: absolute;
  top: 0;
  left: 0;
  border-radius: 50%;
  cursor: pointer;
  user-select: none;
  touch-action: none;
  will-change: transform, width, height;
  transition: z-index 0s;
  z-index: 10;
}

.bubble-node--hover {
  z-index: 100;
}

.bubble-node--dim {
  opacity: 0.3;
  filter: blur(2px);
  transition: opacity 0.4s ease, filter 0.4s ease;
}

/* Halo Glow */
.bubble-node__halo {
  position: absolute;
  inset: -20%;
  background: radial-gradient(circle, var(--bubble-glow) 0%, transparent 70%);
  opacity: 0;
  transition: opacity 0.4s ease;
  pointer-events: none;
  mix-blend-mode: screen;
}

.bubble-node--hover .bubble-node__halo {
  opacity: 0.6;
}

/* Glass Sphere */
.bubble-node__glass {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.1) 0%, rgba(255, 255, 255, 0.01) 100%);
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
  border: 1px solid rgba(255, 255, 255, 0.15);
  box-shadow:
    inset 0 0 20px rgba(255, 255, 255, 0.05),
    0 10px 20px rgba(0, 0, 0, 0.2);
  overflow: hidden;
  transition: all 0.4s cubic-bezier(0.175, 0.885, 0.32, 1.275);
  display: flex;
  align-items: center;
  justify-content: center;
  text-align: center;
}

.bubble-node--hover .bubble-node__glass {
  transform: scale(1.35);
  background: linear-gradient(135deg, rgba(20, 30, 50, 0.85) 0%, rgba(10, 20, 40, 0.95) 100%);
  border-color: var(--bubble-glow);
  box-shadow:
    0 0 30px var(--bubble-glow),
    inset 0 0 20px rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(12px);
}

/* Internal Fog */
.bubble-node__fog {
  position: absolute;
  inset: 0;
  background: radial-gradient(circle at 30% 30%, rgba(255, 255, 255, 0.1), transparent 60%);
  opacity: 0.5;
  pointer-events: none;
}

/* Content */
.bubble-node__content {
  position: relative;
  z-index: 2;
  padding: 15%;
  color: var(--text-primary);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
}

.bubble-node__tag {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 1px;
  color: var(--bubble-glow);
  margin-bottom: 4px;
  opacity: 0.8;
  font-weight: 600;
}

.bubble-node__title {
  font-size: 14px;
  font-weight: 600;
  line-height: 1.3;
  text-shadow: 0 2px 4px rgba(0, 0, 0, 0.5);
  transition: transform 0.3s ease;
}

.bubble-node--hover .bubble-node__title {
  transform: translateY(-5px);
  font-size: 13px; /* Slightly smaller to fit more content */
}

/* Reveal Section */
.bubble-node__reveal {
  height: 0;
  opacity: 0;
  overflow: hidden;
  transition: all 0.3s ease;
  transform: translateY(10px);
}

.bubble-node--hover .bubble-node__reveal {
  height: auto;
  opacity: 1;
  transform: translateY(0);
  margin-top: 8px;
}

.bubble-node__subtitle {
  font-size: 11px;
  color: var(--text-secondary);
  margin-bottom: 8px;
  line-height: 1.4;
}

.bubble-node__divider {
  width: 20px;
  height: 1px;
  background: var(--bubble-glow);
  margin: 6px auto;
  opacity: 0.5;
}

.bubble-node__keywords {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 4px;
}

.bubble-node__keyword {
  font-size: 9px;
  color: var(--text-tertiary);
  background: rgba(255, 255, 255, 0.05);
  padding: 2px 6px;
  border-radius: 4px;
}
</style>

