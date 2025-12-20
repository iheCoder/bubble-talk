/**
 * BubbleTalk WebSocket Gateway Client
 * 适配新的后端 WebSocket 流式接口
 */

export class BubbleTalkGateway {
  constructor(sessionId) {
    this.sessionId = sessionId;
    this.ws = null;
    this.audioContext = null;
    this.mediaRecorder = null;
    this.isRecording = false;
    this.pendingChunks = [];
    this.pendingSamples = 0;
    this.minChunkSamples = 0;

    // 事件回调
    this.onASRPartial = null; // 实时转写
    this.onASRFinal = null;   // 最终转写
    this.onTTSStarted = null;  // TTS 开始
    this.onTTSCompleted = null; // TTS 完成
    this.onAudioData = null;   // 接收音频数据
    this.onError = null;       // 错误
    this.onConnected = null;   // 连接成功
    this.onDisconnected = null; // 断开连接
    this.onSpeechStarted = null; // VAD 说话开始
    this.onSpeechStopped = null; // VAD 说话结束
  }

  /**
   * 连接到 WebSocket
   */
  async connect() {
    return new Promise((resolve, reject) => {
      const wsUrl = `ws://localhost:8080/api/sessions/${this.sessionId}/stream`;
      console.log('[Gateway] Connecting to:', wsUrl);

      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => {
        console.log('[Gateway] ✅ WebSocket connected');
        if (this.onConnected) this.onConnected();
        resolve();
      };

      this.ws.onerror = (error) => {
        console.error('[Gateway] ❌ WebSocket error:', error);
        if (this.onError) this.onError(error);
        reject(error);
      };

      this.ws.onclose = (event) => {
        console.log('[Gateway] WebSocket closed:', event.code, event.reason);
        if (this.onDisconnected) this.onDisconnected(event);
      };

      this.ws.onmessage = (event) => {
        if (event.data instanceof Blob) {
          // Binary frame - 音频数据
          this._handleAudioData(event.data);
        } else {
          // Text frame - JSON 事件
          this._handleTextMessage(event.data);
        }
      };
    });
  }

  /**
   * 处理文本消息（JSON 事件）
   */
  _handleTextMessage(data) {
    try {
      const message = JSON.parse(data);
      console.log('[Gateway] Event received:', message.type);

      switch (message.type) {
        case 'asr_partial':
          if (this.onASRPartial) this.onASRPartial(message.text);
          break;
        case 'asr_final':
          if (this.onASRFinal) this.onASRFinal(message.text);
          break;
        case 'tts_started':
          if (this.onTTSStarted) this.onTTSStarted(message.metadata);
          break;
        case 'tts_completed':
          if (this.onTTSCompleted) this.onTTSCompleted(message.metadata);
          break;
        case 'assistant_text':
          console.log('[Gateway] Assistant text:', message.text);
          if (this.onAssistantText) {
            this.onAssistantText(message.text, message.metadata);
          }
          break;
        case 'error':
          console.error('[Gateway] Server error:', message.error);
          if (this.onError) this.onError(new Error(message.error));
          break;
        case 'speech_started':
          if (this.onSpeechStarted) this.onSpeechStarted();
          break;
        case 'speech_stopped':
          if (this.onSpeechStopped) this.onSpeechStopped();
          break;
        default:
          console.log('[Gateway] Unhandled event type:', message.type);
      }
    } catch (err) {
      console.error('[Gateway] Failed to parse message:', err);
    }
  }

  /**
   * 处理音频数据
   */
  async _handleAudioData(blob) {
    console.log('[Gateway] Audio data received:', blob.size, 'bytes');
    if (this.onAudioData) {
      this.onAudioData(blob);
    }
  }

  /**
   * 开始录音
   */
  async startRecording() {
    if (this.isRecording) {
      console.warn('[Gateway] Already recording');
      return;
    }

    console.log('[Gateway] Starting recording...');

    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          sampleRate: 24000 // OpenAI Realtime 需要 24kHz
        }
      });

      // 使用 AudioContext 处理音频
      this.audioContext = new (window.AudioContext || window.webkitAudioContext)({
        sampleRate: 24000
      });
      if (this.audioContext.state !== 'running') {
        await this.audioContext.resume();
      }
      const sampleRate = this.audioContext.sampleRate || 24000;
      const minAudioMs = 120;
      this.minChunkSamples = Math.ceil((sampleRate * minAudioMs) / 1000);
      this.pendingChunks = [];
      this.pendingSamples = 0;

      const source = this.audioContext.createMediaStreamSource(stream);

      // 创建 ScriptProcessor 用于获取原始音频数据
      const processor = this.audioContext.createScriptProcessor(4096, 1, 1);

      processor.onaudioprocess = (e) => {
        if (!this.isRecording || !this.ws || this.ws.readyState !== WebSocket.OPEN) {
          return;
        }

        // 获取音频数据（Float32Array）
        const inputData = e.inputBuffer.getChannelData(0);

        // 转换为 Int16Array (PCM16)
        const pcm16 = new Int16Array(inputData.length);
        for (let i = 0; i < inputData.length; i++) {
          // Float32 范围是 -1 到 1，转换为 Int16 范围 -32768 到 32767
          const s = Math.max(-1, Math.min(1, inputData[i]));
          pcm16[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
        }

        if (pcm16.length === 0) {
          return;
        }

        this._enqueueAudio(pcm16);
      };

      source.connect(processor);
      processor.connect(this.audioContext.destination);

      // 保存引用以便停止时清理
      this.audioStream = stream;
      this.audioProcessor = processor;
      this.audioSource = source;

      this.isRecording = true;
      console.log('[Gateway] ✅ Recording started (PCM16 24kHz)');
    } catch (err) {
      console.error('[Gateway] ❌ Failed to start recording:', err);
      if (this.onError) this.onError(err);
      throw err;
    }
  }

  /**
   * 停止录音
   */
  stopRecording() {
    if (!this.isRecording) {
      return;
    }

    console.log('[Gateway] Stopping recording...');

    // 停止音频处理
    if (this.audioProcessor) {
      this.audioProcessor.disconnect();
      this.audioProcessor = null;
    }

    if (this.audioSource) {
      this.audioSource.disconnect();
      this.audioSource = null;
    }

    if (this.audioStream) {
      this.audioStream.getTracks().forEach(track => track.stop());
      this.audioStream = null;
    }

    if (this.audioContext && this.audioContext.state !== 'closed') {
      this.audioContext.close();
      this.audioContext = null;
    }

    this.isRecording = false;
    this.pendingChunks = [];
    this.pendingSamples = 0;
    this.minChunkSamples = 0;
    console.log('[Gateway] ✅ Recording stopped');
  }

  _enqueueAudio(pcm16) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return;
    }
    if (!this.minChunkSamples) {
      this.ws.send(pcm16.buffer);
      return;
    }

    this.pendingChunks.push({ data: pcm16, offset: 0 });
    this.pendingSamples += pcm16.length;

    while (this.pendingSamples >= this.minChunkSamples) {
      const chunk = new Int16Array(this.minChunkSamples);
      let written = 0;

      while (written < chunk.length && this.pendingChunks.length > 0) {
        const head = this.pendingChunks[0];
        const available = head.data.length - head.offset;
        const toCopy = Math.min(available, chunk.length - written);
        chunk.set(head.data.subarray(head.offset, head.offset + toCopy), written);
        head.offset += toCopy;
        written += toCopy;
        this.pendingSamples -= toCopy;

        if (head.offset >= head.data.length) {
          this.pendingChunks.shift();
        }
      }

      this.ws.send(chunk.buffer);
      console.log('[Gateway] PCM16 audio sent:', chunk.length, 'samples');
    }
  }

  /**
   * 发送答题事件
   */
  sendQuizAnswer(questionId, answer) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.error('[Gateway] WebSocket not connected');
      return;
    }

    const message = {
      type: 'quiz_answer',
      event_id: `evt_${Date.now()}`,
      question_id: questionId,
      answer: answer,
      client_ts: new Date().toISOString()
    };

    console.log('[Gateway] Sending quiz answer:', message);
    this.ws.send(JSON.stringify(message));
  }

  /**
   * 发送插话中断事件
   */
  sendBargeIn() {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.error('[Gateway] WebSocket not connected');
      return;
    }

    const message = {
      type: 'barge_in',
      event_id: `evt_${Date.now()}`,
      client_ts: new Date().toISOString()
    };

    console.log('[Gateway] Sending barge-in');
    this.ws.send(JSON.stringify(message));
  }

  /**
   * 发送退出请求
   */
  sendExitRequest() {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.error('[Gateway] WebSocket not connected');
      return;
    }

    const message = {
      type: 'exit_requested',
      event_id: `evt_${Date.now()}`,
      client_ts: new Date().toISOString()
    };

    console.log('[Gateway] Sending exit request');
    this.ws.send(JSON.stringify(message));
  }

  /**
   * 通知服务端：World 已进入，导演可以主动开场
   */
  sendWorldEntered(metadata = {}) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.error('[Gateway] WebSocket not connected');
      return;
    }

    const message = {
      type: 'world_entered',
      event_id: `evt_${Date.now()}`,
      metadata,
      client_ts: new Date().toISOString()
    };

    console.log('[Gateway] Sending world_entered:', message);
    this.ws.send(JSON.stringify(message));
  }

  /**
   * 断开连接
   */
  disconnect() {
    console.log('[Gateway] Disconnecting...');

    this.stopRecording();

    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }

    console.log('[Gateway] ✅ Disconnected');
  }
}

/**
 * 音频播放器
 */
export class AudioPlayer {
  constructor() {
    this.audioContext = null;
    this.audioQueue = [];
    this.isPlaying = false;
    this.gainNode = null;
    this.nextStartTime = 0;
    this.outputSampleRate = 24000;
    this.onDrain = null;
    this._drainTimer = null;
  }

  async init() {
    if (!this.audioContext) {
      this.audioContext = new (window.AudioContext || window.webkitAudioContext)();
      this.gainNode = this.audioContext.createGain();
      this.gainNode.connect(this.audioContext.destination);
      console.log('[AudioPlayer] Initialized');
    }
  }

  async playAudioBlob(blob) {
    await this.init();

    try {
      const arrayBuffer = await blob.arrayBuffer();
      const pcm16 = new Int16Array(arrayBuffer);
      if (pcm16.length === 0) {
        return;
      }

      // OpenAI Realtime 输出是 raw PCM16，需要手动转成 AudioBuffer
      const float32 = new Float32Array(pcm16.length);
      for (let i = 0; i < pcm16.length; i++) {
        float32[i] = pcm16[i] / 32768;
      }

      const audioBuffer = this.audioContext.createBuffer(1, float32.length, this.outputSampleRate);
      audioBuffer.copyToChannel(float32, 0);

      const source = this.audioContext.createBufferSource();
      source.buffer = audioBuffer;
      source.connect(this.gainNode);

      const now = this.audioContext.currentTime;
      if (this.nextStartTime < now) {
        this.nextStartTime = now;
      }
      source.start(this.nextStartTime);
      this.nextStartTime += audioBuffer.duration;

      this._scheduleDrainCheck();
      console.log('[AudioPlayer] Playing audio:', audioBuffer.duration, 'seconds');
    } catch (err) {
      console.error('[AudioPlayer] Failed to play audio:', err);
    }
  }

  _scheduleDrainCheck() {
    if (!this.audioContext) {
      return;
    }
    if (this._drainTimer) {
      window.clearTimeout(this._drainTimer);
      this._drainTimer = null;
    }
    const remainingSec = Math.max(0, this.nextStartTime - this.audioContext.currentTime);
    const delayMs = Math.ceil(remainingSec * 1000) + 30;
    this._drainTimer = window.setTimeout(() => {
      if (!this.audioContext) {
        return;
      }
      const stillRemaining = this.nextStartTime - this.audioContext.currentTime;
      if (stillRemaining > 0.05) {
        this._scheduleDrainCheck();
        return;
      }
      if (this.onDrain) {
        this.onDrain();
      }
    }, delayMs);
  }

  setVolume(volume) {
    if (this.gainNode) {
      this.gainNode.gain.value = volume;
    }
  }
}
