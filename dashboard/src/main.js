import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import { createPinia } from 'pinia'
import App from './App.vue'

// Auth guard function
function requireAuth(to, from, next) {
  const token = localStorage.getItem('herald_token')
  if (token) {
    next()
  } else {
    next('/login')
  }
}

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('./views/Login.vue'),
    meta: { public: true }
  },
  {
    path: '/',
    name: 'Dashboard',
    component: () => import('./views/Dashboard.vue'),
    beforeEnter: requireAuth
  },
  {
    path: '/providers',
    name: 'Providers',
    component: () => import('./views/Providers.vue'),
    beforeEnter: requireAuth
  },
  {
    path: '/providers/:name/config',
    name: 'ProviderConfig',
    component: () => import('./views/ProviderConfig.vue'),
    beforeEnter: requireAuth
  },
  {
    path: '/workers',
    name: 'Workers',
    component: () => import('./views/Workers.vue'),
    beforeEnter: requireAuth
  },
  {
    path: '/send',
    name: 'Send',
    component: () => import('./views/Send.vue'),
    beforeEnter: requireAuth
  },
  {
    path: '/logs',
    name: 'Logs',
    component: () => import('./views/Logs.vue'),
    beforeEnter: requireAuth
  }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

const pinia = createPinia()

const app = createApp(App)
app.use(router)
app.use(pinia)
app.mount('#app')
