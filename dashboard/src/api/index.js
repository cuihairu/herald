import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
})

// Add API Key from localStorage if available
api.interceptors.request.use(config => {
  const apiKey = localStorage.getItem('herald_api_key')
  if (apiKey) {
    config.headers['X-API-Key'] = apiKey
  }
  return config
})

api.interceptors.response.use(
  response => response.data,
  error => {
    console.error('API Error:', error)
    return Promise.reject(error)
  }
)

export const heraldApi = {
  // 获取状态
  getStatus: () => api.get('/status'),

  // 获取 Providers
  getProviders: () => api.get('/providers'),

  // 获取 Workers
  getWorkers: () => api.get('/workers'),

  // 获取队列状态
  getQueue: () => api.get('/queue'),

  // 启用 Provider
  enableProvider: (name) => api.post(`/providers/${name}/enable`),

  // 禁用 Provider
  disableProvider: (name) => api.post(`/providers/${name}/disable`),

  // 发送通知
  sendNotify: (data) => api.post('/notify', data),

  // 发送事件
  sendEvent: (data) => api.post('/events', data),

  // 获取日志
  getLogs: (params) => api.get('/logs', { params }),

  // 获取日志统计
  getLogsStats: () => api.get('/logs/stats'),

  // 获取 Provider 配置
  getProviderConfig: (name) => api.get(`/config/${name}`),

  // 更新 Provider 配置
  updateProviderConfig: (name, data) => api.put(`/config/${name}`, data)
}
