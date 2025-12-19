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
    <div class="bubble-node__glass">
      <div class="bubble-node__title">{{ bubble.title }}</div>
      <div class="bubble-node__subtitle">{{ bubble.subtitle }}</div>
      <div class="bubble-node__tag">{{ bubble.tag }}</div>
      <div class="bubble-node__preview">
        <div class="bubble-node__detail">{{ bubble.detail }}</div>
        <div class="bubble-node__keywords">
          <span v-for="keyword in bubble.keywords" :key="keyword" class="bubble-node__keyword">{{ keyword }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
