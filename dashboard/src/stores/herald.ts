import { create } from 'zustand'
import { heraldApi } from '../api'

// Series requests are user-cancelable by changing the window: each call
// gets a sequence number, and a late response from an older window is
// discarded instead of overwriting the current selection.
let seriesRequestSeq = 0

interface HeraldState {
  status: any
  overview: any
  providers: any[]
  workers: any[]
  queue: { size: number }
  logStats: any
  metrics: any[]
  sendSeries: any[]
  seriesWindow: string
  seriesLoading: boolean
  rules: any[]
  groups: any[]
  rosters: any[]
  loading: boolean
  error: string | null

  fetchStatus: () => Promise<void>
  fetchOverview: () => Promise<void>
  fetchSeries: (window: string) => Promise<void>
  fetchProviders: () => Promise<void>
  fetchWorkers: () => Promise<void>
  fetchQueue: () => Promise<void>
  fetchLogStats: () => Promise<void>
  fetchRules: () => Promise<void>
  fetchGroups: () => Promise<void>
  fetchRosters: () => Promise<void>
  sendNotify: (data: any) => Promise<any>
  enableProvider: (name: string) => Promise<void>
  disableProvider: (name: string) => Promise<void>
}

export const useHeraldStore = create<HeraldState>((set, get) => ({
  status: null,
  overview: null,
  providers: [],
  workers: [],
  queue: { size: 0 },
  logStats: null,
  metrics: [],
  sendSeries: [],
  seriesWindow: '6h',
  seriesLoading: false,
  rules: [],
  groups: [],
  rosters: [],
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

  fetchOverview: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getOverview()
      set({ overview: res.data })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchSeries: async (window: string) => {
    const requestSeq = ++seriesRequestSeq
    set({ seriesLoading: true, seriesWindow: window, error: null })
    try {
      const [metricsRes, sendRes] = await Promise.all([
        heraldApi.getMetricsHistory(window),
        heraldApi.getLogsTimeseries(window),
      ])
      if (requestSeq !== seriesRequestSeq) return
      set({ metrics: metricsRes.data.samples || [], sendSeries: sendRes.data.points || [] })
    } catch (err: any) {
      if (requestSeq !== seriesRequestSeq) return
      set({ error: err.message })
    } finally {
      if (requestSeq === seriesRequestSeq) {
        set({ seriesLoading: false })
      }
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

  fetchRules: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getRules()
      set({ rules: res.data.rules || [] })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchGroups: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getGroups()
      set({ groups: res.data.groups || [] })
    } catch (err: any) {
      set({ error: err.message })
    } finally {
      set({ loading: false })
    }
  },

  fetchRosters: async () => {
    set({ loading: true, error: null })
    try {
      const res = await heraldApi.getRosters()
      set({ rosters: res.data.rosters || [] })
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
