import { defineStore } from 'pinia'
import { ref } from 'vue'
import { heraldApi } from '../api'

export const useHeraldStore = defineStore('herald', () => {
  const status = ref(null)
  const providers = ref([])
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

  return {
    status,
    providers,
    loading,
    error,
    fetchStatus,
    fetchProviders,
    sendNotify,
    sendEvent
  }
})
