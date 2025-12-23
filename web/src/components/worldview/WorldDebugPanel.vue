<script setup>
const props = defineProps({
  isConnected: {
    type: Boolean,
    default: false,
  },
  transcript: {
    type: Array,
    default: () => [],
  },
  connectionError: {
    type: String,
    default: '',
  },
})

const emit = defineEmits(['clear-error'])
</script>

<template>
  <div v-if="isConnected && transcript.length > 0" class="realtime-debug">
    <div class="realtime-debug__title">调试日志</div>
    <div class="realtime-debug__content">
      <div v-for="(evt, i) in transcript.slice(-3)" :key="i" class="debug-item">
        {{ evt.type }}
      </div>
    </div>
  </div>

  <div v-if="connectionError" class="error-toast">
    {{ connectionError }}
    <button @click="emit('clear-error')">✕</button>
  </div>
</template>

<style scoped>
.realtime-debug {
  position: absolute;
  top: 80px;
  left: 24px;
  width: 200px;
  background: rgba(0, 0, 0, 0.5);
  border-radius: 8px;
  padding: 12px;
  font-size: 10px;
  color: rgba(255, 255, 255, 0.6);
  pointer-events: none;
  z-index: 5;
}

.realtime-debug__title {
  font-weight: 600;
  margin-bottom: 4px;
  text-transform: uppercase;
  opacity: 0.5;
}

.debug-item {
  margin-bottom: 2px;
  font-family: monospace;
}

.error-toast {
  position: absolute;
  top: 80px;
  left: 50%;
  transform: translateX(-50%);
  background: rgba(255, 80, 80, 0.9);
  color: white;
  padding: 8px 16px;
  border-radius: 8px;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  z-index: 100;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
}

.error-toast button {
  background: transparent;
  border: none;
  color: white;
  cursor: pointer;
  opacity: 0.8;
  padding: 0;
}
</style>
