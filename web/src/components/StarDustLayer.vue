<script setup>
import { onMounted, ref, watch } from 'vue'

const props = defineProps({
  width: Number,
  height: Number,
})

const canvasRef = ref(null)
const stars = ref([])

const seedStars = () => {
  const count = Math.floor((props.width * props.height) / 18000)
  stars.value = Array.from({ length: count }).map(() => ({
    x: Math.random() * props.width,
    y: Math.random() * props.height,
    r: Math.random() * 1.2 + 0.4,
    a: Math.random() * 0.4 + 0.2,
  }))
}

const draw = () => {
  const canvas = canvasRef.value
  if (!canvas) return
  const ctx = canvas.getContext('2d')
  if (!ctx) return
  canvas.width = props.width
  canvas.height = props.height
  ctx.clearRect(0, 0, props.width, props.height)
  ctx.fillStyle = 'rgba(255,255,255,0.8)'

  stars.value.forEach((star) => {
    ctx.globalAlpha = star.a
    ctx.beginPath()
    ctx.arc(star.x, star.y, star.r, 0, Math.PI * 2)
    ctx.fill()
  })
  ctx.globalAlpha = 1
}

onMounted(() => {
  seedStars()
  draw()
})

watch(
  () => [props.width, props.height],
  () => {
    seedStars()
    draw()
  },
)
</script>

<template>
  <canvas ref="canvasRef" class="star-dust-layer"></canvas>
</template>
