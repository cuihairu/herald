import axios from 'axios'

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
})

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('herald_token')
  if (token) {
    config.headers['Authorization'] = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (res) => res.data,
  (err) => {
    console.error('API Error:', err)
    return Promise.reject(err)
  }
)

export const heraldApi = {
  getStatus: () => api.get('/status') as any,
  getProviders: () => api.get('/providers') as any,
  getWorkers: () => api.get('/workers') as any,
  getQueue: () => api.get('/queue') as any,
  enableProvider: (name: string) => api.post(`/providers/${name}/enable`) as any,
  disableProvider: (name: string) => api.post(`/providers/${name}/disable`) as any,
  sendNotify: (data: any) => api.post('/notify', data) as any,
  getLogs: (params: any) => api.get('/logs', { params }) as any,
  getLogsStats: () => api.get('/logs/stats') as any,
  getProviderConfig: (name: string) => api.get(`/config/${name}`) as any,
  updateProviderConfig: (name: string, data: any) => api.put(`/config/${name}`, data) as any,
  login: (data: { username: string; password: string }) => api.post('/auth/login', data) as any,
}
