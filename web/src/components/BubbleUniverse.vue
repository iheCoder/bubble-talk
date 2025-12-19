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
    // Distribute bubbles in a more organic, galaxy-like spiral
    const angle = (index / count) * Math.PI * 2 + (Math.random() * 0.5)
    const distance = Math.min(universe.width, universe.height) * 0.35
    const anchorX = universe.width * 0.5 + Math.cos(angle) * distance * (0.8 + Math.random() * 0.4)
    const anchorY = universe.height * 0.5 + Math.sin(angle) * distance * (0.8 + Math.random() * 0.4)

    // Varied sizes for depth perception
    const baseSize = 90
    const sizeVariation = Math.random() * 40
    const radius = baseSize + sizeVariation

    return {
      ...bubble,
      x: anchorX,
      y: anchorY,
      anchorX,
      anchorY,
      vx: (Math.random() - 0.5) * 0.2,
      vy: (Math.random() - 0.5) * 0.2,
      radius,
      drift: Math.random() * 100,
      hover: false,
      dim: false,
      mass: radius / 50, // Heavier bubbles move slower
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
  const padding = 120
  const hoveredBubble = universe.hoverId ? universe.bubbles.find((bubble) => bubble.id === universe.hoverId) : null

  universe.bubbles.forEach((bubble, i) => {
    bubble.dim = !!hoveredBubble && hoveredBubble.id !== bubble.id

    // Buoyancy & Drift (Deep Space Physics)
    // Gentle return to anchor
    const spring = 0.0008 / bubble.mass
    const dxAnchor = bubble.anchorX - bubble.x
    const dyAnchor = bubble.anchorY - bubble.y
    bubble.vx += dxAnchor * spring
    bubble.vy += dyAnchor * spring

    // Perlin-like noise for organic drift
    const noiseStrength = 0.08 / bubble.mass
    bubble.vx += Math.sin(time * 0.5 + bubble.drift + i) * noiseStrength
    bubble.vy += Math.cos(time * 0.3 + bubble.drift + i * 0.5) * noiseStrength

    // Mouse Interaction: Magnetic Pull & Repulsion
    if (pointer.active) {
      const dx = bubble.x - pointer.x
      const dy = bubble.y - pointer.y
      const dist = Math.hypot(dx, dy)
      const influenceRadius = 400

      if (dist < influenceRadius) {
        if (universe.hoverId === bubble.id) {
           // Magnetic Pull (Gentle)
           const pullStrength = 0.02
           bubble.vx -= (dx / dist) * pullStrength
           bubble.vy -= (dy / dist) * pullStrength
        } else {
           // Gentle Repulsion for others to clear view
           const pushStrength = (influenceRadius - dist) / influenceRadius * 0.05
           bubble.vx += (dx / dist) * pushStrength
           bubble.vy += (dy / dist) * pushStrength
        }
      }
    }

    // Damping (Water resistance)
    bubble.vx *= 0.96
    bubble.vy *= 0.96

    bubble.x += bubble.vx
    bubble.y += bubble.vy

    // Soft Boundaries
    if (bubble.x < padding) bubble.vx += 0.05
    if (bubble.x > universe.width - padding) bubble.vx -= 0.05
    if (bubble.y < padding) bubble.vy += 0.05
    if (bubble.y > universe.height - padding) bubble.vy -= 0.05
  })

  // Collision Resolution (Soft Nudge)
  for (let i = 0; i < universe.bubbles.length; i++) {
    for (let j = i + 1; j < universe.bubbles.length; j++) {
      const b1 = universe.bubbles[i]
      const b2 = universe.bubbles[j]
      const dx = b2.x - b1.x
      const dy = b2.y - b1.y
      const dist = Math.hypot(dx, dy)
      const minDist = b1.radius + b2.radius + 40 // Extra spacing

      if (dist < minDist) {
        const force = (minDist - dist) * 0.005 // Very soft collision
        const nx = dx / dist
        const ny = dy / dist

        b1.vx -= nx * force
        b1.vy -= ny * force
        b2.vx += nx * force
        b2.vy += ny * force
      }
    }
  }

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
