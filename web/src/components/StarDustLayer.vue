<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps({
  width: Number,
  height: Number,
})

const canvasRef = ref(null)
const stars = ref([])
const frameRef = ref(0)
let lastTime = 0

const seedStars = () => {
  const count = Math.floor((props.width * props.height) / 18000)
  stars.value = Array.from({ length: count }).map(() => ({
    x: Math.random() * props.width,
    y: Math.random() * props.height,
    r: Math.random() * 1.2 + 0.4,
    a: Math.random() * 0.4 + 0.2,
    base: Math.random() * 0.4 + 0.2,
    speed: Math.random() * 0.08 + 0.02,
    twinkle: Math.random() * 1.2 + 0.4,
    phase: Math.random() * Math.PI * 2,
  }))
}

const draw = (time) => {
  const canvas = canvasRef.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const delta = lastTime ? Math.min(32, time - lastTime) / 1000 : 0
  lastTime = time
  canvas.width = props.width
  canvas.height = props.height
  ctx.clearRect(0, 0, props.width, props.height)
  ctx.fillStyle = 'rgba(255,255,255,0.8)'

  stars.value.forEach((star) => {
    star.y += star.speed * 20 * delta
    if (star.y > props.height + 8) {
      star.y = -8
      star.x = Math.random() * props.width
    }
    const twinkle = Math.sin(time / 1000 * star.twinkle + star.phase) * 0.18
    ctx.globalAlpha = Math.max(0.1, Math.min(1, star.base + twinkle))
    ctx.beginPath()
    ctx.arc(star.x, star.y, star.r, 0, Math.PI * 2)
    ctx.fill()
  })
  ctx.globalAlpha = 1
  frameRef.value = requestAnimationFrame(draw)
}

onMounted(() => {
  seedStars()
  frameRef.value = requestAnimationFrame(draw)
})

watch(
  () => [props.width, props.height],
  () => {
    seedStars()
    lastTime = 0
  },
)

onBeforeUnmount(() => {
  cancelAnimationFrame(frameRef.value)
})
</script>

<template>
  <canvas ref="canvasRef" class="star-dust-layer"></canvas>
</template>
