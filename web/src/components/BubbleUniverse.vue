<script setup>
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import BubbleNode from './BubbleNode.vue'
import StarDustLayer from './StarDustLayer.vue'

const props = defineProps({
  bubbles: {
    type: Array,
    required: true,
  },
})

const emit = defineEmits(['select'])

const containerRef = ref(null)
const frameRef = ref(0)
const pointer = reactive({
  active: false,
  x: 0,
  y: 0,
})
const universe = reactive({
  width: 0,
  height: 0,
  bubbles: [],
  dragId: null,
})

const initBubbles = () => {
  const count = props.bubbles.length
  universe.bubbles = props.bubbles.map((bubble, index) => {
    const angle = (index / count) * Math.PI * 2
    const radius = 60 + (index % 4) * 6
    return {
      ...bubble,
      x: universe.width * 0.2 + Math.cos(angle) * universe.width * 0.25 + Math.random() * 60,
      y: universe.height * 0.3 + Math.sin(angle) * universe.height * 0.2 + Math.random() * 40,
      vx: (Math.random() - 0.5) * 0.4,
      vy: (Math.random() - 0.5) * 0.4,
      radius,
      drift: Math.random() * 10,
      hover: false,
    }
  })
}

const update = () => {
  const time = performance.now() / 1000
  const padding = 90

  universe.bubbles.forEach((bubble, i) => {
    const buoyancy = -0.04
    bubble.vy += buoyancy

    const noise = Math.sin(time + bubble.drift + i) * 0.03
    bubble.vx += noise
    bubble.vy += Math.cos(time * 0.8 + bubble.drift) * 0.02

    if (pointer.active) {
      const dx = bubble.x - pointer.x
      const dy = bubble.y - pointer.y
      const dist = Math.max(30, Math.hypot(dx, dy))
      if (dist < 220) {
        const force = (220 - dist) / 220
        bubble.vx += (dx / dist) * force * 0.2
        bubble.vy += (dy / dist) * force * 0.2
      }
    }

    universe.bubbles.forEach((other, j) => {
      if (i === j) return
      const dx = bubble.x - other.x
      const dy = bubble.y - other.y
      const dist = Math.max(1, Math.hypot(dx, dy))
      const minDist = bubble.radius + other.radius + 14
      if (dist < minDist) {
        const force = (minDist - dist) / minDist
        bubble.vx += (dx / dist) * force * 0.1
        bubble.vy += (dy / dist) * force * 0.1
      }
    })

    if (universe.dragId === bubble.id) {
      bubble.x += (pointer.x - bubble.x) * 0.18
      bubble.y += (pointer.y - bubble.y) * 0.18
      bubble.vx *= 0.6
      bubble.vy *= 0.6
    } else {
      bubble.vx *= 0.92
      bubble.vy *= 0.92
      bubble.x += bubble.vx
      bubble.y += bubble.vy
    }

    if (bubble.x < padding) {
      bubble.x = padding
      bubble.vx *= -0.4
    }
    if (bubble.x > universe.width - padding) {
      bubble.x = universe.width - padding
      bubble.vx *= -0.4
    }
    if (bubble.y < padding) {
      bubble.y = padding
      bubble.vy *= -0.4
    }
    if (bubble.y > universe.height - padding) {
      bubble.y = universe.height - padding
      bubble.vy *= -0.4
    }
  })

  frameRef.value = requestAnimationFrame(update)
}

const handleResize = () => {
  if (!containerRef.value) return
  const rect = containerRef.value.getBoundingClientRect()
  universe.width = rect.width
  universe.height = rect.height
  if (!universe.bubbles.length) {
    initBubbles()
  }
}

const handlePointerMove = (event) => {
  const rect = containerRef.value?.getBoundingClientRect()
  if (!rect) return
  pointer.x = event.clientX - rect.left
  pointer.y = event.clientY - rect.top
}

const handlePointerLeave = () => {
  pointer.active = false
  universe.dragId = null
}

const handlePointerDown = (bubble, event) => {
  pointer.active = true
  if (containerRef.value) {
    const rect = containerRef.value.getBoundingClientRect()
    pointer.x = event.clientX - rect.left
    pointer.y = event.clientY - rect.top
  }
  universe.dragId = bubble.id
}

const handlePointerUp = () => {
  universe.dragId = null
}

const handleSelect = (payload) => {
  emit('select', payload)
}

onMounted(() => {
  handleResize()
  window.addEventListener('resize', handleResize)
  frameRef.value = requestAnimationFrame(update)
})

onBeforeUnmount(() => {
  window.removeEventListener('resize', handleResize)
  cancelAnimationFrame(frameRef.value)
})
</script>

<template>
  <div
    ref="containerRef"
    class="bubble-universe"
    @pointermove="handlePointerMove"
    @pointerdown="pointer.active = true"
    @pointerleave="handlePointerLeave"
    @pointerup="handlePointerUp"
  >
    <StarDustLayer :width="universe.width" :height="universe.height" />

    <BubbleNode
      v-for="bubble in universe.bubbles"
      :key="bubble.id"
      :bubble="bubble"
      @select="handleSelect"
      @drag-start="handlePointerDown"
      @drag-end="handlePointerUp"
    />
  </div>
</template>
