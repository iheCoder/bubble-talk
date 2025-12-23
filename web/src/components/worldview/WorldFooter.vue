<script setup>
const props = defineProps({
  input: {
    type: String,
    default: '',
  },
  showKeyboardButton: {
    type: Boolean,
    default: false,
  },
})

const emit = defineEmits(['update:input', 'send'])

const openInput = () => {
  emit('update:input', ' ')
}

const closeInput = () => {
  emit('update:input', '')
}
</script>

<template>
  <footer class="world-footer">
    <div v-if="showKeyboardButton" class="footer-controls">
      <button class="btn-keyboard glass-panel" @click="openInput">
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <rect x="2" y="4" width="20" height="16" rx="2"/>
          <path d="M6 8h.01M10 8h.01M14 8h.01M18 8h.01M6 12h.01M10 12h.01M14 12h.01M18 12h.01M6 16h.01M10 16h.01M14 16h.01M18 16h.01"/>
        </svg>
      </button>
    </div>

    <div v-if="input" class="input-overlay glass-panel">
      <input
        :value="input"
        type="text"
        placeholder="输入你的想法..."
        @input="emit('update:input', $event.target.value)"
        @keydown.enter="emit('send')"
        autofocus
      />
      <button class="btn-close-input" @click="closeInput">✕</button>
    </div>
  </footer>
</template>

<style scoped>
.world-footer {
  padding: 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  z-index: 10;
  pointer-events: none;
}

.footer-controls {
  pointer-events: auto;
  position: absolute;
  bottom: 24px;
  right: 24px;
}

.btn-keyboard {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: rgba(255, 255, 255, 0.8);
  cursor: pointer;
  transition: all 0.2s;
}

.btn-keyboard:hover {
  background: rgba(255, 255, 255, 0.15);
  transform: translateY(-2px);
}

.input-overlay {
  position: fixed;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  width: 90%;
  max-width: 600px;
  padding: 8px;
  border-radius: 12px;
  display: flex;
  gap: 8px;
  z-index: 100;
  box-shadow: 0 10px 30px rgba(0,0,0,0.5);
}

.input-overlay input {
  flex: 1;
  background: transparent;
  border: none;
  color: white;
  font-size: 16px;
  padding: 8px 12px;
  outline: none;
}

.btn-close-input {
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  padding: 0 12px;
  font-size: 18px;
}

.btn-close-input:hover {
  color: white;
}
</style>
