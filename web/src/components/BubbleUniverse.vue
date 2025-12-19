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
  prevWidth: 0,
  prevHeight: 0,
  bubbles: [],
  dragId: null,
  hoverId: null,
})

const initBubbles = () => {
  const count = props.bubbles.length
  universe.bubbles = props.bubbles.map((bubble, index) => {
    const angle = (index / count) * Math.PI * 2
    const radius = 70 + (index % 4) * 8
    const anchorX = universe.width * 0.2 + Math.cos(angle) * universe.width * 0.3 + Math.random() * 60
    const anchorY = universe.height * 0.45 + Math.sin(angle) * universe.height * 0.25 + Math.random() * 60
    return {
      ...bubble,
      x: anchorX,
      y: anchorY,
      anchorX,
      anchorY,
      vx: (Math.random() - 0.5) * 0.4,
      vy: (Math.random() - 0.5) * 0.4,
      radius,
      drift: Math.random() * 10,
      hover: false,
      dim: false,
    }
  })

  const padding = 90
  for (let step = 0; step < 60; step += 1) {
    universe.bubbles.forEach((bubble, i) => {
      universe.bubbles.forEach((other, j) => {
        if (i === j) return
        const dx = bubble.x - other.x
        const dy = bubble.y - other.y
        const dist = Math.max(1, Math.hypot(dx, dy))
        const minDist = bubble.radius + other.radius + 24
        if (dist < minDist) {
          const push = (minDist - dist) * 0.5
          bubble.x += (dx / dist) * push
          bubble.y += (dy / dist) * push
        }
      })
      bubble.x = Math.min(Math.max(bubble.x, padding), universe.width - padding)
      bubble.y = Math.min(Math.max(bubble.y, padding), universe.height - padding)
      bubble.anchorX = bubble.x
      bubble.anchorY = bubble.y
    })
  }
}

const update = () => {
  const time = performance.now() / 1000
  const padding = 90
  const hoveredBubble = universe.hoverId ? universe.bubbles.find((bubble) => bubble.id === universe.hoverId) : null

  universe.bubbles.forEach((bubble, i) => {
    bubble.dim = !!hoveredBubble && hoveredBubble.id !== bubble.id
    const spring = 0.0024
    const dxAnchor = bubble.anchorX - bubble.x
    const dyAnchor = bubble.anchorY - bubble.y
    bubble.vx += dxAnchor * spring
    bubble.vy += dyAnchor * spring

    const noise = Math.sin(time + bubble.drift + i) * 0.02
    bubble.vx += noise
    bubble.vy += Math.cos(time * 0.7 + bubble.drift) * 0.02

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

    if (hoveredBubble && hoveredBubble.id !== bubble.id) {
      const dx = bubble.x - hoveredBubble.x
      const dy = bubble.y - hoveredBubble.y
      const dist = Math.max(40, Math.hypot(dx, dy))
      if (dist < 260) {
        const force = (260 - dist) / 260
        bubble.vx += (dx / dist) * force * 0.35
        bubble.vy += (dy / dist) * force * 0.35
      }
    }

    universe.bubbles.forEach((other, j) => {
      if (i === j) return
      const dx = bubble.x - other.x
      const dy = bubble.y - other.y
      const dist = Math.max(1, Math.hypot(dx, dy))
      const minDist = bubble.radius + other.radius + 32
      if (dist < minDist) {
        const force = (minDist - dist) / minDist
        const strength = bubble.id === universe.hoverId || other.id === universe.hoverId ? 0.2 : 0.14
        bubble.vx += (dx / dist) * force * strength
        bubble.vy += (dy / dist) * force * strength
      }
    })

    if (universe.dragId === bubble.id) {
      bubble.x += (pointer.x - bubble.x) * 0.18
      bubble.y += (pointer.y - bubble.y) * 0.18
      bubble.vx *= 0.6
      bubble.vy *= 0.6
    } else if (universe.hoverId === bubble.id && pointer.active) {
      bubble.x += (pointer.x - bubble.x) * 0.14
      bubble.y += (pointer.y - bubble.y) * 0.14
      bubble.vx *= 0.35
      bubble.vy *= 0.35
    } else {
      bubble.vx *= 0.9
      bubble.vy *= 0.9
      bubble.x += bubble.vx
      bubble.y += bubble.vy
    }

    const xMin = padding
    const xMax = universe.width - padding
    const yMin = padding
    const yMax = universe.height - padding
    if (bubble.x < xMin) {
      bubble.x = xMin
      bubble.vx *= -0.35
    }
    if (bubble.x > xMax) {
      bubble.x = xMax
      bubble.vx *= -0.35
    }
    if (bubble.y < yMin) {
      bubble.y = yMin
      bubble.vy *= -0.35
    }
    if (bubble.y > yMax) {
      bubble.y = yMax
      bubble.vy *= -0.35
    }
  })

  frameRef.value = requestAnimationFrame(update)
}

const handleResize = () => {
  if (!containerRef.value) return
  const rect = containerRef.value.getBoundingClientRect()
  const prevWidth = universe.width || rect.width
  const prevHeight = universe.height || rect.height
  universe.width = rect.width
  universe.height = rect.height
  if (!universe.bubbles.length) {
    initBubbles()
  } else if (prevWidth && prevHeight) {
    const scaleX = rect.width / prevWidth
    const scaleY = rect.height / prevHeight
    universe.bubbles.forEach((bubble) => {
      bubble.x *= scaleX
      bubble.y *= scaleY
      bubble.anchorX *= scaleX
      bubble.anchorY *= scaleY
    })
  }
  universe.prevWidth = rect.width
  universe.prevHeight = rect.height
}

const handlePointerMove = (event) => {
  const rect = containerRef.value?.getBoundingClientRect()
  if (!rect) return
  pointer.active = true
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

const handleHoverStart = (bubble) => {
  bubble.hover = true
  universe.hoverId = bubble.id
  pointer.active = true
  pointer.x = bubble.x
  pointer.y = bubble.y
}

const handleHoverEnd = (bubble) => {
  bubble.hover = false
  if (universe.hoverId === bubble.id) {
    universe.hoverId = null
  }
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
      @hover-start="handleHoverStart"
      @hover-end="handleHoverEnd"
    />
  </div>
</template>
