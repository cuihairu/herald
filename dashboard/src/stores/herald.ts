import { create } from 'zustand'
import { heraldApi } from '../api'

interface HeraldState {
  status: any
  providers: any[]
  workers: any[]
  queue: { size: number }
  logStats: any
  loading: boolean
  error: string | null

  fetchStatus: () => Promise<void>
  fetchProviders: () => Promise<void>
  fetchWorkers: () => Promise<void>
  fetchQueue: () => Promise<void>
  fetchLogStats: () => Promise<void>
  sendNotify: (data: any) => Promise<any>
  enableProvider: (name: string) => Promise<void>
  disableProvider: (name: string) => Promise<void>
}

export const useHeraldStore = create<HeraldState>((set, get) => ({
  status: null,
  providers: [],
  workers: [],
  queue: { size: 0 },
  logStats: null,
  loading: false,
  error: null,

  fetchStatus: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getStatus()
      set({ status: res.data })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchProviders: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getProviders()
      set({ providers: res.data.providers })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchWorkers: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getWorkers()
      set({ workers: res.data.workers || [] })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchQueue: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getQueue()
      set({ queue: res.data })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchLogStats: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getLogsStats()
      set({ logStats: res.data })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  sendNotify: async (data: any) => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.sendNotify(data)
      return res
    } catch (err: any) {
      set({ error: err.message })
      throw err
    } finally {
      set({ loading: false })
    }
  },

  enableProvider: async (name: string) => {
    set({ loading: true, error: null })
    try {
      await heraldApi.enableProvider(name)
      await get().fetchProviders()
    } catch (err: any) {
      set({ error: err.message })
      throw err
    } finally {
      set({ loading: false })
    }
  },

  disableProvider: async (name: string) => {
    set({ loading: true, error: null })
    try {
      await heraldApi.disableProvider(name)
      await get().fetchProviders()
    } catch (err: any) {
      set({ error: err.message })
      throw err
    } finally {
      set({ loading: false })
    }
  },
}))
