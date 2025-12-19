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
  const count = Math.floor((props.width * props.height) / 12000) // More stars
  stars.value = Array.from({ length: count }).map(() => {
    const depth = Math.random() // 0 = far, 1 = near
    return {
      x: Math.random() * props.width,
      y: Math.random() * props.height,
      r: (Math.random() * 1.5 + 0.5) * (depth * 0.5 + 0.5),
      a: Math.random() * 0.5 + 0.1,
      base: Math.random() * 0.5 + 0.1,
      speed: (Math.random() * 0.05 + 0.01) * (depth * 2 + 0.5), // Parallax speed
      twinkle: Math.random() * 2 + 0.5,
      phase: Math.random() * Math.PI * 2,
      color: Math.random() > 0.8 ? '#7cffdb' : (Math.random() > 0.8 ? '#ffc78c' : '#ffffff') // Mint, Amber, White
    }
  })
}

const draw = (time) => {
  const canvas = canvasRef.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  const delta = lastTime ? Math.min(32, time - lastTime) / 1000 : 0
  lastTime = time

  // Don't clear rect every frame if we want trails, but for crisp stars:
  ctx.clearRect(0, 0, props.width, props.height)

  stars.value.forEach((star) => {
    star.y -= star.speed * 10 * delta // Move upwards slowly like bubbles/dust
    if (star.y < -10) {
      star.y = props.height + 10
      star.x = Math.random() * props.width
    }

    const twinkle = Math.sin(time / 1000 * star.twinkle + star.phase) * 0.2
    ctx.globalAlpha = Math.max(0.05, Math.min(0.8, star.base + twinkle))
    ctx.fillStyle = star.color

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
