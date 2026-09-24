import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useWebSocket } from './useWebSocket'

// 手动驱动的 WebSocket 替身：jsdom 没有 WebSocket 实现。
class FakeWebSocket {
  static instances: FakeWebSocket[] = []
  static CONNECTING = 0
  static OPEN = 1
  static CLOSING = 2
  static CLOSED = 3

  url: string
  readyState = FakeWebSocket.CONNECTING
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: (() => void) | null = null
  onerror: ((e: unknown) => void) | null = null
  sent: string[] = []

  constructor(url: string) {
    this.url = url
    FakeWebSocket.instances.push(this)
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED
  }

  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }
  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) })
  }
  messageRaw(data: string) {
    this.onmessage?.({ data })
  }
  drop() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.()
  }
}

const { WebSocket: RealWS } = globalThis

describe('useWebSocket', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeWebSocket.instances = []
    vi.stubGlobal('WebSocket', FakeWebSocket)
  })
  afterEach(() => {
    vi.stubGlobal('WebSocket', RealWS)
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('connects to the /ws endpoint of the current host on mount', () => {
    renderHook(() => useWebSocket())
    expect(FakeWebSocket.instances).toHaveLength(1)
    // jsdom 的 location 是 http://localhost:3000/ → ws 协议 + 同 host
    expect(FakeWebSocket.instances[0].url).toBe('ws://localhost:3000/ws')
  })

  it('notifies onConnect and resets attempts when the socket opens', () => {
    const onConnect = vi.fn()
    renderHook(() => useWebSocket({ onConnect }))
    const ws = FakeWebSocket.instances[0]
    ws.drop() // 先制造一次失败，让 attempts 变 1
    act(() => vi.advanceTimersByTime(3000))
    FakeWebSocket.instances[1].open()
    expect(onConnect).toHaveBeenCalledTimes(1)
  })

  it('delivers parsed JSON messages', () => {
    const onMessage = vi.fn()
    renderHook(() => useWebSocket({ onMessage }))
    const ws = FakeWebSocket.instances[0]
    act(() => ws.open())
    act(() => ws.message({ type: 'worker', id: 'w1' }))
    expect(onMessage).toHaveBeenCalledWith({ type: 'worker', id: 'w1' })
  })

  it('logs but does not crash on a non-JSON message', () => {
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    renderHook(() => useWebSocket())
    const ws = FakeWebSocket.instances[0]
    act(() => ws.open())
    expect(() => act(() => ws.messageRaw('{broken'))).not.toThrow()
    expect(errSpy).toHaveBeenCalled()
  })

  it('logs on socket errors', () => {
    const errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    renderHook(() => useWebSocket())
    act(() => FakeWebSocket.instances[0].onerror?.(new Event('x')))
    expect(errSpy).toHaveBeenCalled()
  })

  it('reconnects after a drop, at most 5 times', () => {
    const onDisconnect = vi.fn()
    renderHook(() => useWebSocket({ onDisconnect }))
    expect(FakeWebSocket.instances).toHaveLength(1)

    for (let i = 1; i <= 5; i++) {
      act(() => FakeWebSocket.instances[i - 1].drop())
      expect(onDisconnect).toHaveBeenCalledTimes(i)
      act(() => vi.advanceTimersByTime(3000))
      expect(FakeWebSocket.instances).toHaveLength(i + 1)
    }

    // 第 6 次掉线：重连预算已耗尽，不再新建连接。
    act(() => FakeWebSocket.instances[5].drop())
    act(() => vi.advanceTimersByTime(3000))
    expect(FakeWebSocket.instances).toHaveLength(6)
  })

  it('sends JSON only while the socket is open', () => {
    const { result } = renderHook(() => useWebSocket())
    const ws = FakeWebSocket.instances[0]

    result.current.send({ a: 1 }) // CONNECTING，不发送
    act(() => ws.open())
    result.current.send({ a: 1 })
    expect(ws.sent).toEqual([JSON.stringify({ a: 1 })])
  })

  it('disconnect clears timers, closes the socket and stops reconnecting', () => {
    const { result, unmount } = renderHook(() => useWebSocket())
    const ws = FakeWebSocket.instances[0]
    act(() => ws.drop()) // 安排了一次 3s 后的重连
    result.current.disconnect()
    act(() => vi.advanceTimersByTime(10000))
    expect(FakeWebSocket.instances).toHaveLength(1) // 重连被掐掉

    unmount() // cleanup 再次调用 disconnect，须幂等
    expect(FakeWebSocket.instances).toHaveLength(1)
  })

  it('disconnects on unmount', () => {
    const { unmount } = renderHook(() => useWebSocket())
    const ws = FakeWebSocket.instances[0]
    unmount()
    expect(ws.readyState).toBe(FakeWebSocket.CLOSED)
  })
})
