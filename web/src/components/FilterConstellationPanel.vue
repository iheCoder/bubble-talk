<script setup>
const props = defineProps({
  open: {
    type: Boolean,
    default: false,
  },
})
const emit = defineEmits(['close'])
</script>

<template>
  <div class="filter-panel" :class="{ 'filter-panel--open': open }">
    <div class="filter-panel__header">
      <span>探索星盘</span>
      <button class="ghost-button" @click="emit('close')">关闭</button>
    </div>
    <div class="filter-panel__group">
      <div class="filter-panel__label">领域</div>
      <div class="filter-panel__chips">
        <button class="chip">经济</button>
        <button class="chip">心理</button>
        <button class="chip">学习</button>
        <button class="chip">方法</button>
        <button class="chip">沟通</button>
      </div>
    </div>
    <div class="filter-panel__group">
      <div class="filter-panel__label">难度</div>
      <div class="filter-panel__chips">
        <button class="chip">入门</button>
        <button class="chip">进阶</button>
        <button class="chip">挑战</button>
      </div>
    </div>
    <div class="filter-panel__group">
      <div class="filter-panel__label">形式偏好</div>
      <div class="filter-panel__chips">
        <button class="chip">更多题</button>
        <button class="chip">更多故事</button>
        <button class="chip">更多图解</button>
      </div>
    </div>
  </div>
</template>
