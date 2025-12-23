<script setup>
const props = defineProps({
  title: {
    type: String,
    default: '今日话题',
  },
  tag: {
    type: String,
    default: '主题',
  },
  expertTag: {
    type: String,
    default: '',
  },
  isConnected: {
    type: Boolean,
    default: false,
  },
})

const emit = defineEmits(['exit', 'toggle-connection'])
</script>

<template>
  <header class="world-header glass-panel">
    <div class="world-header__left">
      <button class="btn-icon" @click="emit('exit')">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M19 12H5M12 19l-7-7 7-7" />
        </svg>
      </button>
      <div class="world-title">
        <h1>{{ title }}</h1>
        <span class="world-tag">{{ tag }} · {{ expertTag }}</span>
      </div>
    </div>

    <div class="world-header__right">
      <button
        class="realtime-button"
        :class="{ 'is-connected': isConnected }"
        @click="emit('toggle-connection')"
      >
        <span class="status-dot"></span>
        {{ isConnected ? '语音已连接' : '连接语音' }}
      </button>
    </div>
  </header>
</template>

<style scoped>
.world-header {
  padding: 16px 24px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  z-index: 10;
  border-bottom: none;
}

.world-header__left {
  display: flex;
  align-items: center;
  gap: 16px;
}

.world-title h1 {
  font-size: 18px;
  font-weight: 600;
  margin: 0;
  line-height: 1.2;
  color: rgba(255, 255, 255, 0.9);
}

.world-tag {
  font-size: 12px;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 1px;
}

.btn-icon {
  background: rgba(255, 255, 255, 0.1);
  border: none;
  color: var(--text-primary);
  cursor: pointer;
  padding: 8px;
  border-radius: 50%;
  transition: background 0.2s;
  backdrop-filter: blur(4px);
}

.realtime-button {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 16px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.7);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s;
  backdrop-filter: blur(4px);
}

.realtime-button:hover {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  transform: translateY(-1px);
}

.realtime-button.is-connected {
  background: rgba(124, 255, 219, 0.15);
  border-color: rgba(124, 255, 219, 0.3);
  color: #7cffdb;
}

.status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.realtime-button.is-connected .status-dot {
  box-shadow: 0 0 8px currentColor;
}
</style>
