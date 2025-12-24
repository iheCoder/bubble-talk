import { ref, computed } from 'vue'
import { BubbleTalkGateway, AudioPlayer } from '../api/gateway.js'

export function useVoiceConnection(sessionId, options = {}) {
  const { onMessage = () => {}, onError = () => {} } = options

  const gateway = ref(null)
  const audioPlayer = ref(null)
  const isConnecting = ref(false)
  const isConnected = ref(false)
  const isMicActive = ref(false)
  const isMuted = ref(false)
  const connectionError = ref('')
  const transcript = ref([])
  const partialTranscript = ref('')

  const connect = async () => {
    if (isConnecting.value || isConnected.value) return

    try {
      isConnecting.value = true
      connectionError.value = ''

      gateway.value = new BubbleTalkGateway(sessionId.value)
      audioPlayer.value = new AudioPlayer()

      gateway.value.onConnected = async () => {
        isConnected.value = true
        isConnecting.value = false
        console.log('[VoiceConnection] Connected')

        if (!isMuted.value) {
          try {
            await gateway.value.startRecording()
            isMicActive.value = true
          } catch (err) {
            console.error('[VoiceConnection] Recording failed:', err)
          }
        }

        onMessage('system', 'Voice connection established')
      }

      gateway.value.onDisconnected = () => {
        isConnected.value = false
        isMicActive.value = false
      }

      gateway.value.onASRPartial = (text) => {
        partialTranscript.value = text
      }

      gateway.value.onASRFinal = (text) => {
        partialTranscript.value = ''
        transcript.value.push({ type: 'user', text, time: new Date() })
        onMessage('user', text)
      }

      gateway.value.onTTSStarted = () => {
        console.log('[VoiceConnection] AI speaking')
      }

      gateway.value.onTTSCompleted = () => {
        console.log('[VoiceConnection] AI finished')
      }

      gateway.value.onTTSInterrupted = () => {
        try {
          audioPlayer.value?.interrupt()
        } catch (err) {
          console.warn('[VoiceConnection] Failed to interrupt audio:', err)
        }
      }

      gateway.value.onSpeechStarted = () => {
        try {
          audioPlayer.value?.interrupt()
        } catch (err) {
          console.warn('[VoiceConnection] Failed to interrupt audio on speech_started:', err)
        }
      }

      gateway.value.onAudioData = async (blob) => {
        await audioPlayer.value.playAudioBlob(blob)
      }

      gateway.value.onAssistantText = (text, metadata) => {
        const role = metadata?.role || 'expert'
        const beat = metadata?.beat

        transcript.value.push({
          type: 'assistant',
          role,
          text,
          beat,
          time: new Date()
        })

        onMessage(role, text, metadata)
      }

      gateway.value.onError = (error) => {
        connectionError.value = error.message
        console.error('[VoiceConnection] Error:', error)
        isConnecting.value = false
        onError(error)
      }

      await gateway.value.connect()
    } catch (err) {
      connectionError.value = err.message
      isConnecting.value = false
      isConnected.value = false
      console.error('[VoiceConnection] Connection failed:', err)
      onError(err)
    }
  }

  const disconnect = () => {
    isMicActive.value = false
    if (gateway.value) {
      gateway.value.stopRecording()
      gateway.value.disconnect()
      gateway.value = null
    }
    if (audioPlayer.value) {
      audioPlayer.value = null
    }
    isConnected.value = false
  }

  const toggleMute = async () => {
    isMuted.value = !isMuted.value
    if (gateway.value) {
      if (isMuted.value) {
        await gateway.value.stopRecording()
        isMicActive.value = false
      } else if (isConnected.value) {
        await gateway.value.startRecording()
        isMicActive.value = true
      }
    }
  }

  return {
    isConnecting: computed(() => isConnecting.value),
    isConnected: computed(() => isConnected.value),
    isMicActive: computed(() => isMicActive.value),
    isMuted: computed(() => isMuted.value),
    connectionError: computed(() => connectionError.value),
    transcript: computed(() => transcript.value),
    partialTranscript: computed(() => partialTranscript.value),
    connect,
    disconnect,
    toggleMute,
  }
}
