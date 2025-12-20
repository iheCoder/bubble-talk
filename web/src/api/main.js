import axios from 'axios'

const api = axios.create({
  baseURL: 'http://localhost:8080/api', // 假设后端运行在 8080 端口
  timeout: 10000,
})

export const getBubbles = async () => {
  const response = await api.get('/bubbles')
  return response.data
}

export const createSession = async (entryId) => {
  const response = await api.post('/sessions', { entry_id: entryId })
  return response.data
}

