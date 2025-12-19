<script setup>
import { computed, ref } from 'vue'
import BubbleUniverse from './BubbleUniverse.vue'
import FilterConstellationPanel from './FilterConstellationPanel.vue'

const emit = defineEmits(['enter-world'])
const props = defineProps({
  portalActive: {
    type: Boolean,
    default: false,
  },
})

const showFilters = ref(false)
const bubbleSeed = [
  {
    id: 1,
    title: '周末加班值不值？',
    subtitle: '机会成本藏在时间里',
    tag: '经济',
    glow: 'rgba(88, 214, 255, 0.7)',
  },
  {
    id: 2,
    title: '为什么你学了又忘？',
    subtitle: '遗忘曲线与间隔重复',
    tag: '学习',
    glow: 'rgba(124, 255, 219, 0.7)',
  },
  {
    id: 3,
    title: '价格涨了你反而买更多？',
    subtitle: '韦伯伦效应的暗号',
    tag: '行为',
    glow: 'rgba(255, 196, 110, 0.7)',
  },
  {
    id: 4,
    title: '如何从容面对考试焦虑？',
    subtitle: '认知重评的微光',
    tag: '心理',
    glow: 'rgba(155, 166, 255, 0.7)',
  },
  {
    id: 5,
    title: '为什么计划总被打乱？',
    subtitle: '执行意图的设计',
    tag: '效率',
    glow: 'rgba(118, 245, 169, 0.7)',
  },
  {
    id: 6,
    title: '自控力耗尽是真的吗？',
    subtitle: '意志的补给线',
    tag: '心理',
    glow: 'rgba(255, 168, 209, 0.7)',
  },
  {
    id: 7,
    title: '为什么会冲动消费？',
    subtitle: '即时奖励的幻象',
    tag: '经济',
    glow: 'rgba(138, 220, 255, 0.7)',
  },
  {
    id: 8,
    title: '你真的理解因果吗？',
    subtitle: '相关不等于因果',
    tag: '方法',
    glow: 'rgba(197, 255, 161, 0.7)',
  },
  {
    id: 9,
    title: '工作中如何提出好问题？',
    subtitle: '提问框架的力量',
    tag: '沟通',
    glow: 'rgba(255, 212, 148, 0.7)',
  },
  {
    id: 10,
    title: '先做还是先学？',
    subtitle: '实践与理论的对齐',
    tag: '学习',
    glow: 'rgba(122, 255, 239, 0.7)',
  },
]

const bubbles = computed(() => bubbleSeed)

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

    <BubbleUniverse :bubbles="bubbles" @select="handleEnter" />

    <FilterConstellationPanel :open="showFilters" @close="showFilters = false" />
  </div>
</template>
