import { defineStore } from 'pinia'
import { ref } from 'vue'
import { heraldApi } from '../api'

export const useHeraldStore = defineStore('herald', () => {
  const status = ref(null)
  const providers = ref([])
  const workers = ref([])
  const queue = ref({ size: 0 })
  const loading = ref(false)
  const error = ref(null)

  async function fetchStatus() {
    loading.value = true
    error.value = null
    try {
      const response = await heraldApi.getStatus()
      status.value = response.data
    } catch (err) {
      error.value = err.message
    } finally {
      loading.value = false
    }
  }

  async function fetchProviders() {
    loading.value = true
    error.value = null
    try {
      const response = await heraldApi.getProviders()
      providers.value = response.data.providers
    } catch (err) {
      error.value = err.message
    } finally {
      loading.value = false
    }
  }

  async function fetchWorkers() {
    loading.value = true
    error.value = null
    try {
      const response = await heraldApi.getWorkers()
      workers.value = response.data.workers || []
    } catch (err) {
      error.value = err.message
    } finally {
      loading.value = false
    }
  }

  async function fetchQueue() {
    loading.value = true
    error.value = null
    try {
      const response = await heraldApi.getQueue()
      queue.value = response.data
    } catch (err) {
      error.value = err.message
    } finally {
      loading.value = false
    }
  }

  async function sendNotify(data) {
    loading.value = true
    error.value = null
    try {
      const response = await heraldApi.sendNotify(data)
      return response
    } catch (err) {
      error.value = err.message
      throw err
    } finally {
      loading.value = false
    }
  }

  async function sendEvent(data) {
    loading.value = true
    error.value = null
    try {
      const response = await heraldApi.sendEvent(data)
      return response
    } catch (err) {
      error.value = err.message
      throw err
    } finally {
      loading.value = false
    }
  }

  async function enableProvider(name) {
    loading.value = true
    error.value = null
    try {
      await heraldApi.enableProvider(name)
      await fetchProviders()
    } catch (err) {
      error.value = err.message
      throw err
    } finally {
      loading.value = false
    }
  }

  async function disableProvider(name) {
    loading.value = true
    error.value = null
    try {
      await heraldApi.disableProvider(name)
      await fetchProviders()
    } catch (err) {
      error.value = err.message
      throw err
    } finally {
      loading.value = false
    }
  }

  return {
    status,
    providers,
    workers,
    queue,
    loading,
    error,
    fetchStatus,
    fetchProviders,
    fetchWorkers,
    fetchQueue,
    sendNotify,
    sendEvent,
    enableProvider,
    disableProvider
  }
})
