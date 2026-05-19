import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useWebSocket } from './websocket'

export const useRealtimeStore = defineStore('realtime', () => {
  const connected = ref(false)
  const taskUpdates = ref([])
  const workerUpdates = ref([])

  const { connect, disconnect, on, off, send } = useWebSocket()

  function init() {
    // Setup event listeners
    on('connected', () => {
      connected.value = true
    })

    on('disconnected', () => {
      connected.value = false
    })

    on('message', (data) => {
      handleMessage(data)
    })

    // Connect to WebSocket
    connect()
  }

  function handleMessage(data) {
    switch (data.type) {
      case 'task.update':
        taskUpdates.value.unshift(data)
        // Keep only recent updates
        if (taskUpdates.value.length > 100) {
          taskUpdates.value = taskUpdates.value.slice(0, 100)
        }
        break

      case 'worker.connected':
      case 'worker.disconnected':
      case 'worker.heartbeat':
        workerUpdates.value.unshift(data)
        if (workerUpdates.value.length > 100) {
          workerUpdates.value = workerUpdates.value.slice(0, 100)
        }
        break

      case 'provider.updated':
        // Provider status changed, trigger refresh
        window.dispatchEvent(new CustomEvent('provider-updated', { detail: data }))
        break
    }
  }

  function cleanup() {
    off('connected')
    off('disconnected')
    off('message')
    disconnect()
  }

  return {
    connected,
    taskUpdates,
    workerUpdates,
    init,
    cleanup,
    send
  }
})
