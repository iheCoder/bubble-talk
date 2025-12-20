<script setup>
import { computed, ref, onMounted } from 'vue'
import BubbleUniverse from './BubbleUniverse.vue'
import FilterConstellationPanel from './FilterConstellationPanel.vue'
import { getBubbles } from '../api/main'

const emit = defineEmits(['enter-world'])
const props = defineProps({
  portalActive: {
    type: Boolean,
    default: false,
  },
})

const showFilters = ref(false)
const bubbleSeed = ref([])
const loading = ref(true)
const error = ref(null)

onMounted(async () => {
  try {
    const data = await getBubbles()
    bubbleSeed.value = data.map((b, index) => ({
      id: index + 1,
      entry_id: b.entry_id,
      title: b.title,
      subtitle: b.subtitle,
      tag: b.tag,
      glow: b.color,
      detail: b.description,
      keywords: b.keywords,
    }))
  } catch (err) {
    console.error('Failed to fetch bubbles:', err)
    error.value = '无法加载泡泡宇宙，请检查网络连接。'
  } finally {
    loading.value = false
  }
})

const bubbles = computed(() => {
  return bubbleSeed.value
})

const handleEnter = (payload) => {
  emit('enter-world', payload)
}

const toggleFilters = () => {
  showFilters.value = !showFilters.value
}
</script>

<template>
  <div class="home-view" :class="{ 'home-view--dim': portalActive }">
    <header class="home-header">
      <div>
        <div class="home-kicker">BubbleTalk · Deep Space Classroom</div>
        <h1 class="home-title">泡泡宇宙</h1>
        <p class="home-subtitle">选择一个人生问题，推开新世界。</p>
      </div>
      <button class="filter-button" @click="toggleFilters">
        <span class="filter-button__icon">✶</span>
        <span>星盘</span>
      </button>
    </header>

    <div v-if="loading" class="loading-state">
      <div class="loading-spinner"></div>
      <p>正在连接泡泡宇宙...</p>
    </div>

    <div v-else-if="error" class="error-state">
      <p>{{ error }}</p>
      <button @click="location.reload()">重试</button>
    </div>

    <BubbleUniverse v-else :bubbles="bubbles" @select="handleEnter" />

    <FilterConstellationPanel :open="showFilters" @close="showFilters = false" />
  </div>
</template>

<style scoped>
.loading-state, .error-state {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  text-align: center;
  color: rgba(255, 255, 255, 0.7);
}

.loading-spinner {
  width: 40px;
  height: 40px;
  border: 3px solid rgba(255, 255, 255, 0.1);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 1s linear infinite;
  margin: 0 auto 1rem;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
</style>

