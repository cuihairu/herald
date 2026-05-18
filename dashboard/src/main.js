import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import { createPinia } from 'pinia'
import App from './App.vue'

const routes = [
  {
    path: '/',
    name: 'Dashboard',
    component: () => import('./views/Dashboard.vue')
  },
  {
    path: '/providers',
    name: 'Providers',
    component: () => import('./views/Providers.vue')
  },
  {
    path: '/workers',
    name: 'Workers',
    component: () => import('./views/Workers.vue')
  },
  {
    path: '/send',
    name: 'Send',
    component: () => import('./views/Send.vue')
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
