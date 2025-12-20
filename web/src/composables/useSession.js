import { ref } from 'vue'

const API_BASE_URL = 'http://localhost:8080/api'

export function useSession() {
  const sessionId = ref(null)
  const isCreating = ref(false)
  const error = ref(null)

  const createSession = async (entryId) => {
    if (isCreating.value) return sessionId.value

    try {
      isCreating.value = true
      error.value = null

      const resp = await fetch(`${API_BASE_URL}/sessions`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ entry_id: entryId }),
      })

      if (!resp.ok) {
        throw new Error(`Failed to create session: ${resp.status}`)
      }

      const data = await resp.json()
      sessionId.value = data.session_id

      return sessionId.value
    } catch (err) {
      error.value = err.message
      console.error('[Session] Failed to create session:', err)
      throw err
    } finally {
      isCreating.value = false
    }
  }

  const ensureSession = async (existingSessionId, entryId) => {
    if (existingSessionId) {
      sessionId.value = existingSessionId
      return existingSessionId
    }

    return await createSession(entryId)
  }

  return {
    sessionId,
    isCreating,
    error,
    createSession,
    ensureSession,
  }
}

