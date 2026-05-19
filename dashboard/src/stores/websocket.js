import { ref } from 'vue'

const ws = ref(null)
const reconnectTimer = ref(null)
const reconnectAttempts = ref(0)
const maxReconnectAttempts = 5
const reconnectDelay = 3000

// Event listeners
const listeners = new Map()

function connect() {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const host = window.location.host
  const wsUrl = `${protocol}//${host}/ws`

  ws.value = new WebSocket(wsUrl)

  ws.value.onopen = () => {
    console.log('WebSocket connected')
    reconnectAttempts.value = 0
    emit('connected')
  }

  ws.value.onmessage = (event) => {
    try {
      const data = JSON.parse(event.data)
      emit('message', data)
    } catch (err) {
      console.error('Failed to parse WebSocket message:', err)
    }
  }

  ws.value.onclose = () => {
    console.log('WebSocket disconnected')
    emit('disconnected')

    // Attempt to reconnect
    if (reconnectAttempts.value < maxReconnectAttempts) {
      reconnectAttempts.value++
      console.log(`Reconnecting... (${reconnectAttempts.value}/${maxReconnectAttempts})`)
      reconnectTimer.value = setTimeout(connect, reconnectDelay)
    }
  }

  ws.value.onerror = (error) => {
    console.error('WebSocket error:', error)
    emit('error', error)
  }
}

function disconnect() {
  if (reconnectTimer.value) {
    clearTimeout(reconnectTimer.value)
    reconnectTimer.value = null
  }

  if (ws.value) {
    ws.value.close()
    ws.value = null
  }

  reconnectAttempts.value = maxReconnectAttempts // Prevent reconnection
}

function send(data) {
  if (ws.value && ws.value.readyState === WebSocket.OPEN) {
    ws.value.send(JSON.stringify(data))
  }
}

function on(event, callback) {
  if (!listeners.has(event)) {
    listeners.set(event, [])
  }
  listeners.get(event).push(callback)
}

function off(event, callback) {
  if (!listeners.has(event)) return

  const callbacks = listeners.get(event)
  const index = callbacks.indexOf(callback)
  if (index > -1) {
    callbacks.splice(index, 1)
  }
}

function emit(event, data) {
  if (!listeners.has(event)) return

  for (const callback of listeners.get(event)) {
    callback(data)
  }
}

export function useWebSocket() {
  return {
    connect,
    disconnect,
    send,
    on,
    off,
    connected: () => ws.value?.readyState === WebSocket.OPEN
  }
}
