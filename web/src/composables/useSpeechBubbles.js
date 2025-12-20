import { ref, computed } from 'vue'

export function useSpeechBubbles() {
  const currentSpeech = ref({})
  const activeRole = ref(null)
  const isThinking = ref(false)
  const timers = []

  const schedule = (fn, delay) => {
    const id = window.setTimeout(fn, delay)
    timers.push(id)
    return id
  }

  const showMessage = (role, text) => {
    currentSpeech.value[role] = null

    setTimeout(() => {
      currentSpeech.value[role] = {
        text,
        timestamp: Date.now()
      }
    }, 10)

    const duration = Math.max(2000, text.length * 100)
    schedule(() => {
      if (currentSpeech.value[role]?.text === text) {
        currentSpeech.value[role] = null
      }
    }, duration + 1000)
  }

  const setActiveRole = (role, thinking = false) => {
    activeRole.value = role
    isThinking.value = thinking
  }

  const cleanup = () => {
    timers.forEach(id => window.clearTimeout(id))
    timers.length = 0
  }

  return {
    currentSpeech: computed(() => currentSpeech.value),
    activeRole: computed(() => activeRole.value),
    isThinking: computed(() => isThinking.value),
    showMessage,
    setActiveRole,
    schedule,
    cleanup,
  }
}

