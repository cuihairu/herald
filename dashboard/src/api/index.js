import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000
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

  // 发送通知
  sendNotify: (data) => api.post('/notify', data),

  // 发送事件
  sendEvent: (data) => api.post('/events', data)
}
