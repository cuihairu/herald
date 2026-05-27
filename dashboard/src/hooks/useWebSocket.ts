import { useEffect, useRef, useCallback } from 'react'

interface UseWebSocketOptions {
  onMessage?: (data: any) => void
  onConnect?: () => void
  onDisconnect?: () => void
}

export function useWebSocket(options: UseWebSocketOptions = {}) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttempts = useRef(0)
  const maxReconnectAttempts = 5
  const reconnectTimer = useRef<ReturnType<typeof setTimeout>>(undefined)
  const optionsRef = useRef(options)
  optionsRef.current = options

  const connect = useCallback(() => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    const wsUrl = `${protocol}//${host}/ws`

    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      console.log('WebSocket connected')
      reconnectAttempts.current = 0
      optionsRef.current.onConnect?.()
    }

    ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        optionsRef.current.onMessage?.(data)
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err)
      }
    }

    ws.onclose = () => {
      console.log('WebSocket disconnected')
      optionsRef.current.onDisconnect?.()

      if (reconnectAttempts.current < maxReconnectAttempts) {
        reconnectAttempts.current++
        console.log(`Reconnecting... (${reconnectAttempts.current}/${maxReconnectAttempts})`)
        reconnectTimer.current = setTimeout(connect, 3000)
      }
    }

    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
    }
  }, [])

  const disconnect = useCallback(() => {
    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current)
    }
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    reconnectAttempts.current = maxReconnectAttempts
  }, [])

  const send = useCallback((data: any) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify(data))
    }
  }, [])

  useEffect(() => {
    connect()
    return () => disconnect()
  }, [connect, disconnect])

  return { send, disconnect }
}
