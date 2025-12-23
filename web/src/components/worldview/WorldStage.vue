<script setup>
import { computed } from 'vue'

const props = defineProps({
  roleMap: {
    type: Object,
    default: () => ({}),
  },
  expertRole: {
    type: Object,
    default: () => ({}),
  },
  activeRole: {
    type: String,
    default: '',
  },
  isAssistantSpeaking: {
    type: Boolean,
    default: false,
  },
  isThinking: {
    type: Boolean,
    default: false,
  },
  currentQuiz: {
    type: Object,
    default: null,
  },
  diagnose: {
    type: Object,
    default: () => ({}),
  },
  toolVisible: {
    type: Boolean,
    default: false,
  },
  toolResolved: {
    type: Boolean,
    default: false,
  },
  selectedOption: {
    type: Number,
    default: null,
  },
  isMuted: {
    type: Boolean,
    default: true,
  },
  isMicActive: {
    type: Boolean,
    default: false,
  },
})

const emit = defineEmits(['toggle-mute', 'hangup', 'answer-quiz'])

const hostRole = computed(() => props.roleMap?.host || {})
</script>

<template>
  <main class="world-stage round-table">
    <div class="table-orbit">
      <div class="table-surface">
        <div class="table-glow"></div>
        <div class="table-grid"></div>
        <div class="table-rim"></div>
        <div class="table-core"></div>
      </div>

      <div class="center-stage">
        <transition name="scale-fade">
          <div v-if="toolVisible" class="content-board glass-panel holographic" :class="{ 'is-resolved': toolResolved }">
            <div class="tool-header">
              <span class="tool-icon">⚡️</span>
              <span class="tool-title">快速检验</span>
            </div>
            <div class="quiz-content" v-if="currentQuiz">
              <div class="quiz-question">{{ currentQuiz.question }}</div>
              <div class="quiz-options">
                <button
                  v-for="(opt, idx) in currentQuiz.options"
                  :key="idx"
                  class="quiz-option"
                  :class="{ 'selected': selectedOption === idx }"
                  @click="emit('answer-quiz', idx)"
                  :disabled="selectedOption !== null"
                >
                  {{ opt }}
                </button>
              </div>
            </div>
            <div class="quiz-content" v-else>
              <div class="quiz-question">加载题目中...</div>
            </div>
          </div>
        </transition>
      </div>

      <div class="seat seat--host" :class="{ 'is-speaking': activeRole === 'host' && isAssistantSpeaking }">
        <div class="avatar-container" :style="{ '--role-color': hostRole.color }">
          <div class="avatar-halo"></div>
          <div class="avatar-ripple"></div>
          <div class="avatar-ripple avatar-ripple--delay"></div>
          <div class="avatar-circle">
            <img v-if="hostRole.avatarImage" :src="hostRole.avatarImage" :alt="hostRole.name" />
            <span v-else>{{ hostRole.avatar }}</span>
          </div>
          <div class="role-label">{{ hostRole.name }}</div>
        </div>
      </div>

      <div class="seat seat--economist" :class="{ 'is-speaking': activeRole === expertRole.id && isAssistantSpeaking }">
        <div class="avatar-container" :style="{ '--role-color': expertRole.color }">
          <div class="avatar-halo"></div>
          <div class="avatar-ripple"></div>
          <div class="avatar-ripple avatar-ripple--delay"></div>
          <div class="avatar-circle">
            <img v-if="expertRole.avatarImage" :src="expertRole.avatarImage" :alt="expertRole.name" />
            <span v-else>{{ expertRole.avatar }}</span>
          </div>
          <div class="role-label">{{ expertRole.name }}</div>
        </div>
        <transition name="fade-slide" mode="out-in">
          <div v-if="activeRole === expertRole.id && isThinking" key="thinking" class="speech-bubble glass-panel speech-bubble--thinking">
            <span class="dot"></span><span class="dot"></span><span class="dot"></span>
          </div>
        </transition>
      </div>

      <div class="seat seat--user" :class="{ 'is-speaking': !isMuted && isMicActive }">
        <div class="user-avatar-area">
            <div class="user-avatar-wrapper">
            <div class="user-avatar-ring" :class="{ 'is-active': !isMuted && isMicActive }"></div>
            <div
              class="user-avatar-wave"
              :class="{ 'is-active': !isMuted && isMicActive, 'is-listening': !isMuted && isMicActive }"
            ></div>
            <div class="user-avatar">
              <img src="https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" alt="User Avatar" />
            </div>
            <div class="user-status-badge" :class="{ 'is-muted': isMuted }">
              {{ isMuted ? '已静音' : '聆听中' }}
            </div>
          </div>

          <div class="user-controls">
            <button class="control-btn" :class="{ 'is-active': isMuted }" @click="emit('toggle-mute')" title="静音/取消静音">
              <svg v-if="!isMuted" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M12 1a3 3 0 0 0-3 3v8a3 3 0 0 0 6 0V4a3 3 0 0 0-3-3z"/>
                <path d="M19 10v2a7 7 0 0 1-14 0v-2"/>
                <line x1="12" y1="19" x2="12" y2="23"/>
                <line x1="8" y1="23" x2="16" y2="23"/>
              </svg>
              <svg v-else width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="1" y1="1" x2="23" y2="23"/>
                <path d="M9 9v3a3 3 0 0 0 5.12 2.12M15 9.34V4a3 3 0 0 0-5.94-.6"/>
                <path d="M17 16.95A7 7 0 0 1 5 12v-2m14 0v2a7 7 0 0 1-.11 1.23"/>
                <line x1="12" y1="19" x2="12" y2="23"/>
                <line x1="8" y1="23" x2="16" y2="23"/>
              </svg>
            </button>
            <button class="control-btn btn-hangup" @click="emit('hangup')" title="结束通话">
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M10.68 13.31a16 16 0 0 0 3.41 2.6l1.27-1.27a2 2 0 0 1 2.11-.45 12.84 12.84 0 0 0 2.81.7 2 2 0 0 1 1.72 2v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.42 19.42 0 0 1-3.33-2.67m-2.67-3.34a19.79 19.79 0 0 1-3.07-8.63A2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72 12.84 12.84 0 0 0 .7 2.81 2 2 0 0 1-.45 2.11L8.09 9.91"/>
                <line x1="23" y1="1" x2="1" y2="23"/>
              </svg>
            </button>
          </div>
        </div>
      </div>
    </div>
  </main>
</template>

<style scoped>
.round-table {
  position: relative;
  width: 100%;
  height: 100%;
  perspective: 1000px;
  overflow: hidden;
}

.table-orbit {
  position: absolute;
  left: 50%;
  top: 56%;
  transform: translate(-50%, -50%);
  width: 90vmin;
  height: 90vmin;
  max-width: 920px;
  max-height: 920px;
}

.table-orbit::before {
  content: '';
  position: absolute;
  inset: 4%;
  border-radius: 50%;
  border: 1px solid rgba(124, 255, 219, 0.08);
  box-shadow: 0 0 40px rgba(124, 255, 219, 0.08);
  opacity: 0.6;
}

.table-orbit::after {
  content: '';
  position: absolute;
  inset: -6%;
  border-radius: 50%;
  border: 1px dashed rgba(255, 255, 255, 0.1);
  opacity: 0.35;
  animation: orbit-spin 40s linear infinite;
}

@keyframes orbit-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.table-surface {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%) rotateX(58deg);
  width: 68vmin;
  height: 68vmin;
  max-width: 680px;
  max-height: 680px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.02) 0%, transparent 70%);
  border: 1px solid rgba(255, 255, 255, 0.05);
  box-shadow:
    0 0 50px rgba(0, 0, 0, 0.5),
    inset 0 0 100px rgba(0, 0, 0, 0.8);
  pointer-events: none;
  z-index: 1;
  transform-style: preserve-3d;
}

.table-glow {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(124, 255, 219, 0.03) 0%, transparent 60%);
  animation: pulse-table 6s infinite ease-in-out;
}

.table-grid {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  background-image:
    radial-gradient(rgba(255, 255, 255, 0.15) 1px, transparent 1px);
  background-size: 8% 8%;
  opacity: 0.2;
  mask-image: radial-gradient(circle, black 40%, transparent 80%);
}

.table-rim {
  position: absolute;
  inset: 4%;
  border-radius: 50%;
  border: 2px solid rgba(124, 255, 219, 0.15);
  box-shadow:
    0 0 30px rgba(124, 255, 219, 0.2),
    inset 0 0 20px rgba(124, 255, 219, 0.15);
  opacity: 0.8;
}

.table-core {
  position: absolute;
  inset: 28%;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(124, 255, 219, 0.1), transparent 70%);
  box-shadow: inset 0 0 25px rgba(124, 255, 219, 0.2);
}

@keyframes pulse-table {
  0%, 100% { opacity: 0.3; transform: scale(1); }
  50% { opacity: 0.6; transform: scale(1.02); }
}

.seat {
  position: absolute;
  display: flex;
  flex-direction: column;
  align-items: center;
  transition: all 0.5s ease;
  z-index: 10;
}

.seat--host {
  top: 18%;
  left: 18%;
  transform: translate(-50%, -50%);
  align-items: flex-start;
}

.seat--economist {
  top: 18%;
  left: 82%;
  transform: translate(-50%, -50%);
  align-items: flex-end;
}

.seat--user {
  top: 92%;
  left: 50%;
  transform: translate(-50%, -50%);
  align-items: center;
  width: auto;
}

.avatar-container {
  position: relative;
  width: 80px;
  height: 80px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.avatar-ripple {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid var(--role-color);
  opacity: 0;
  transform: translate(-50%, -50%) scale(0.85);
  z-index: 0;
  filter: drop-shadow(0 0 12px rgba(255, 255, 255, 0.12));
}

.avatar-ripple--delay {
  animation-delay: 0.6s;
}

.seat.is-speaking .avatar-ripple {
  animation: ripple-wave 1.8s infinite ease-out;
}

.avatar-circle {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: rgba(0, 0, 0, 0.4);
  border: 2px solid var(--role-color);
  color: var(--role-color);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 24px;
  z-index: 2;
  box-shadow: 0 0 20px rgba(0,0,0,0.3);
  transition: transform 0.3s ease;
}

.avatar-circle img {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  object-fit: cover;
}

.avatar-circle span {
  line-height: 1;
}

.seat.is-speaking .avatar-circle {
  transform: scale(1.1);
  box-shadow: 0 0 30px var(--role-color);
}

.avatar-halo {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid var(--role-color);
  opacity: 0;
  z-index: 1;
}

.seat.is-speaking .avatar-halo {
  animation: pulse-halo 2s infinite;
}

@keyframes pulse-halo {
  0% { width: 100%; height: 100%; opacity: 0.8; }
  100% { width: 160%; height: 160%; opacity: 0; }
}

@keyframes ripple-wave {
  0% { transform: translate(-50%, -50%) scale(0.85); opacity: 0.7; }
  70% { opacity: 0.25; }
  100% { transform: translate(-50%, -50%) scale(1.65); opacity: 0; }
}

.role-label {
  margin-top: 8px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.6);
  text-transform: uppercase;
  letter-spacing: 1px;
}

.speech-bubble {
  margin-top: 16px;
  padding: 16px 24px;
  border-radius: 16px;
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(12px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: #fff;
  font-size: 16px;
  line-height: 1.5;
  max-width: 280px;
  box-shadow: 0 4px 20px rgba(0,0,0,0.2);
  position: relative;
}

.seat--host .speech-bubble {
  border-top-left-radius: 4px;
  transform-origin: top left;
}

.seat--economist .speech-bubble {
  border-top-right-radius: 4px;
  transform-origin: top right;
  text-align: right;
}

.seat--user .speech-bubble {
  margin-bottom: 24px;
  margin-top: 0;
  border-bottom-left-radius: 4px;
  border-bottom-right-radius: 4px;
  background: rgba(124, 255, 219, 0.15);
  border-color: rgba(124, 255, 219, 0.3);
}

.speech-bubble--thinking {
  display: flex;
  gap: 4px;
  padding: 12px 20px;
  width: fit-content;
}

.center-stage {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%) rotateX(18deg);
  width: 42vmin;
  height: 42vmin;
  max-width: 460px;
  max-height: 460px;
  display: flex;
  justify-content: center;
  align-items: center;
  z-index: 20;
  pointer-events: auto;
  filter: drop-shadow(0 20px 40px rgba(0, 0, 0, 0.45));
}

.center-stage::before {
  content: '';
  position: absolute;
  inset: 6%;
  border-radius: 50%;
  border: 1px solid rgba(124, 255, 219, 0.2);
  box-shadow: inset 0 0 20px rgba(124, 255, 219, 0.2);
  opacity: 0.6;
  pointer-events: none;
}

.content-board {
  width: 100%;
  height: 100%;
  max-width: 460px;
  max-height: 460px;
  background: rgba(10, 20, 40, 0.2);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 50%;
  padding: 32px;
  backdrop-filter: blur(8px);
  box-shadow:
    0 0 40px rgba(0, 0, 0, 0.35),
    inset 0 0 40px rgba(124, 255, 219, 0.08);
  transform-style: preserve-3d;
  transition: all 0.5s cubic-bezier(0.23, 1, 0.32, 1);
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  text-align: center;
}

.content-board.holographic {
  background:
    radial-gradient(circle at center, rgba(124, 255, 219, 0.18) 0%, transparent 70%),
    radial-gradient(circle at 30% 20%, rgba(255, 255, 255, 0.08), transparent 60%);
  border: 1px solid rgba(124, 255, 219, 0.2);
  box-shadow:
    0 0 60px rgba(124, 255, 219, 0.12),
    inset 0 0 50px rgba(124, 255, 219, 0.08);
}

.tool-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
  color: var(--accent-color, #7cffdb);
  font-weight: 600;
  text-transform: uppercase;
  font-size: 11px;
  letter-spacing: 1px;
  opacity: 0.8;
}

.quiz-question {
  font-size: 17px;
  font-weight: 500;
  margin-bottom: 18px;
  line-height: 1.4;
  color: rgba(255, 255, 255, 0.9);
  max-width: 80%;
}

.quiz-options {
  display: flex;
  flex-direction: column;
  gap: 10px;
  width: 100%;
  align-items: center;
}

.quiz-option {
  width: 78%;
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.06), rgba(124, 255, 219, 0.06));
  border: 1px solid rgba(255, 255, 255, 0.12);
  padding: 12px 18px;
  border-radius: 999px;
  color: rgba(255, 255, 255, 0.8);
  text-align: center;
  cursor: pointer;
  transition: all 0.2s;
  font-size: 14px;
}

.quiz-option:hover {
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.12), rgba(124, 255, 219, 0.14));
  transform: translateY(-2px);
}

.quiz-option.selected {
  background: rgba(124, 255, 219, 0.15);
  border-color: rgba(124, 255, 219, 0.4);
  color: #7cffdb;
}

.user-avatar-area {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  margin-top: 20px;
  position: relative;
}

.user-avatar-wrapper {
  position: relative;
  width: 100px;
  height: 100px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.user-avatar {
  width: 80px;
  height: 80px;
  border-radius: 50%;
  overflow: hidden;
  border: 2px solid rgba(255, 255, 255, 0.2);
  background: #000;
  z-index: 2;
  position: relative;
}

.user-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.user-avatar-ring {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  border: 2px solid var(--accent-color, #7cffdb);
  opacity: 0;
  transform: scale(0.8);
  transition: all 0.2s;
}

.user-avatar-ring.is-active {
  opacity: 0.6;
  animation: pulse-ring 1.5s infinite;
}

.user-avatar-wave {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 100%;
  height: 100%;
  border-radius: 50%;
  border: 2px solid rgba(124, 255, 219, 0.22);
  opacity: 0.18;
  transform: translate(-50%, -50%) scale(0.92);
  animation: ripple-wave 5.8s infinite ease-out;
  transition: opacity 0.2s, border-color 0.2s;
  z-index: 1;
}

.user-avatar-wave.is-listening {
  opacity: 0.38;
  border-color: rgba(124, 255, 219, 0.45);
  animation-duration: 2.8s;
}

.user-avatar-wave.is-active {
  opacity: 0.72;
  border-color: rgba(255, 199, 140, 0.75);
  animation-duration: 1.7s;
}

@keyframes pulse-ring {
  0% { transform: scale(0.9); opacity: 0.8; }
  100% { transform: scale(1.4); opacity: 0; }
}

.user-status-badge {
  position: absolute;
  bottom: -6px;
  background: rgba(124, 255, 219, 0.2);
  border: 1px solid rgba(124, 255, 219, 0.4);
  color: #7cffdb;
  font-size: 10px;
  padding: 2px 8px;
  border-radius: 10px;
  backdrop-filter: blur(4px);
  z-index: 3;
  transition: all 0.3s;
}

.user-status-badge.is-muted {
  background: rgba(255, 100, 100, 0.2);
  border-color: rgba(255, 100, 100, 0.4);
  color: #ff8888;
}

.user-controls {
  display: flex;
  gap: 16px;
}

.control-btn {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.8);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.2s;
  backdrop-filter: blur(4px);
}

.control-btn:hover {
  background: rgba(255, 255, 255, 0.2);
  transform: translateY(-2px);
}

.control-btn.is-active {
  background: rgba(255, 100, 100, 0.2);
  color: #ff8888;
  border-color: rgba(255, 100, 100, 0.4);
}

.control-btn.btn-hangup {
  background: rgba(255, 50, 50, 0.8);
  color: white;
  border: none;
}

.control-btn.btn-hangup:hover {
  background: rgba(255, 80, 80, 1);
}

@media (max-width: 900px) {
  .table-orbit {
    width: 96vmin;
    height: 96vmin;
    top: 58%;
  }

  .table-surface {
    width: 72vmin;
    height: 72vmin;
  }

  .center-stage {
    width: 48vmin;
    height: 48vmin;
  }

  .seat--host {
    top: 16%;
    left: 14%;
  }

  .seat--economist {
    top: 16%;
    left: 86%;
  }

  .seat--user {
    top: 94%;
  }
}

@media (max-width: 600px) {
  .center-stage {
    width: 54vmin;
    height: 54vmin;
    transform: translate(-50%, -50%) rotateX(12deg);
  }

  .content-board {
    padding: 24px;
  }

  .quiz-option {
    width: 86%;
  }
}

.fade-slide-enter-active,
.fade-slide-leave-active {
  transition: all 0.3s ease;
}

.fade-slide-enter-from,
.fade-slide-leave-to {
  opacity: 0;
  transform: translateY(10px);
}

.scale-fade-enter-active,
.scale-fade-leave-active {
  transition: all 0.4s cubic-bezier(0.175, 0.885, 0.32, 1.275);
}

.scale-fade-enter-from,
.scale-fade-leave-to {
  opacity: 0;
  transform: scale(0.9);
}

.dot {
  width: 4px;
  height: 4px;
  background: currentColor;
  border-radius: 50%;
  animation: bounce 1.4s infinite ease-in-out both;
}

.dot:nth-child(1) { animation-delay: -0.32s; }
.dot:nth-child(2) { animation-delay: -0.16s; }

@keyframes bounce {
  0%, 80%, 100% { transform: scale(0); }
  40% { transform: scale(1); }
}
</style>
