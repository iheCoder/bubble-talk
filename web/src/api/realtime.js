// gpt-realtime (OpenAI Realtime API) 的最小 WebRTC 连接封装。
//
// 目标：
// - 让“语音对话”在第一阶段就是默认路径（Conversation First）
// - 浏览器用 WebRTC 直连 OpenAI（音频上行/下行 + DataChannel 事件）
// - 服务端只负责签发 ephemeral key（避免泄漏 OPENAI_API_KEY）
//
// 注意：
// - 这是 MVP 级实现，只做“能连上、能说话、能听到回复、能看到事件”
// - 生产级需要补：重连策略、网络抖动处理、权限错误提示、事件落库等

export async function connectRealtime({
  backendBaseUrl = '',
  sessionId,
  onRemoteStream,
  onEvent,
} = {}) {
  if (!sessionId) throw new Error('sessionId is required')

  // 1) 向后端获取 ephemeral key（短期 client secret）
  const tokenResp = await fetch(`${backendBaseUrl}/api/sessions/${sessionId}/realtime/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  })
  if (!tokenResp.ok) {
    throw new Error(`realtime token request failed: ${tokenResp.status}`)
  }
  const { model, ephemeral_key: ephemeralKey, instructions } = await tokenResp.json()

  // 2) WebRTC：本地麦克风音轨上行
  const pc = new RTCPeerConnection()
  const localStream = await navigator.mediaDevices.getUserMedia({ audio: true })
  for (const track of localStream.getTracks()) {
    pc.addTrack(track, localStream)
  }

  // 3) WebRTC：接收远端音频（TTS 下行）
  pc.ontrack = (ev) => {
    // OpenAI 会把音频作为一个 MediaStreamTrack 下发
    const [remoteStream] = ev.streams
    if (remoteStream && onRemoteStream) onRemoteStream(remoteStream)
  }

  // 4) DataChannel：双向事件（转写、文本、工具调用等）
  const dc = pc.createDataChannel('oai-events')
  dc.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data)
      if (onEvent) onEvent(msg)
    } catch {
      // 忽略非 JSON 的噪声
    }
  }

  // 5) 通过 OpenAI Realtime 走 SDP offer/answer（HTTP 一次性握手）
  const offer = await pc.createOffer()
  await pc.setLocalDescription(offer)

  const sdpResp = await fetch(`https://api.openai.com/v1/realtime?model=${encodeURIComponent(model)}`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${ephemeralKey}`,
      'Content-Type': 'application/sdp',
    },
    body: offer.sdp,
  })
  if (!sdpResp.ok) {
    throw new Error(`openai realtime sdp failed: ${sdpResp.status}`)
  }
  const answerSdp = await sdpResp.text()
  await pc.setRemoteDescription({ type: 'answer', sdp: answerSdp })

  // 6) 最小初始化：把服务端生成的 instructions 设置到 session，并让模型说一句开场白。
  // 说明：更严谨的做法是由“导演/编排器”每轮动态发 session.update + response.create。
  const sendEvent = (event) => {
    if (dc.readyState !== 'open') return
    dc.send(JSON.stringify(event))
  }

  dc.onopen = () => {
    sendEvent({
      type: 'session.update',
      session: {
        instructions,
      },
    })
    sendEvent({
      type: 'response.create',
      response: {
        modalities: ['audio', 'text'],
        instructions: '用一句话欢迎用户，并提出一个非常具体的问题开始测评/对话。',
      },
    })
  }

  return {
    pc,
    dc,
    localStream,
    close: () => {
      try { dc.close() } catch {}
      try { pc.close() } catch {}
      for (const track of localStream.getTracks()) track.stop()
    },
    setMuted: (muted) => {
      localStream.getAudioTracks().forEach((track) => {
        track.enabled = !muted
      })
    },
  }
}
