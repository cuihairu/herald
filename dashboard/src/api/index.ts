import axios from 'axios'

// 导出实例作为测试 seam：单测通过自定义 adapter 走真实的拦截器链路，
// 而不是 mock 掉 axios 本身（externalized CJS 模块的 vi.mock 不可靠）。
export const api = axios.create({
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
  getRules: () => api.get('/rules') as any,
  getRule: (id: string) => api.get(`/rules/${id}`) as any,
  createRule: (data: any) => api.post('/rules', data) as any,
  updateRule: (id: string, data: any) => api.put(`/rules/${id}`, data) as any,
  deleteRule: (id: string) => api.delete(`/rules/${id}`) as any,
  getGroups: () => api.get('/groups') as any,
  getGroup: (id: string) => api.get(`/groups/${id}`) as any,
  createGroup: (data: any) => api.post('/groups', data) as any,
  updateGroup: (id: string, data: any) => api.put(`/groups/${id}`, data) as any,
  deleteGroup: (id: string) => api.delete(`/groups/${id}`) as any,
}
