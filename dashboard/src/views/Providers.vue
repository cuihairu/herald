<template>
  <div class="providers">
    <h1 class="title">Providers</h1>

    <div v-if="store.loading" class="loading">加载中...</div>

    <div v-else-if="store.error" class="error">{{ store.error }}</div>

    <div v-else class="providers-grid">
      <div v-for="provider in store.providers" :key="provider.name" class="provider-card">
        <div class="provider-header">
          <h2 class="provider-name">{{ provider.name }}</h2>
          <span class="provider-badge" :class="provider.type">{{ provider.type }}</span>
        </div>
        <div class="provider-body">
          <div class="provider-row">
            <span class="provider-label">状态:</span>
            <span class="provider-value" :class="{ online: provider.status === 'available' }">
              {{ provider.status }}
            </span>
          </div>
          <div class="provider-row">
            <span class="provider-label">类型:</span>
            <span class="provider-value">{{ provider.type }}</span>
          </div>
          <div v-if="provider.since" class="provider-row">
            <span class="provider-label">启动时间:</span>
            <span class="provider-value">{{ formatDate(provider.since) }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted } from 'vue'
import { useHeraldStore } from '../stores/herald'

const store = useHeraldStore()

function formatDate(dateString) {
  const date = new Date(dateString)
  return date.toLocaleString('zh-CN')
}

onMounted(async () => {
  await store.fetchProviders()
})
</script>

<style scoped>
.title {
  font-size: 1.5rem;
  font-weight: 600;
  margin-bottom: 2rem;
}

.loading, .error {
  padding: 2rem;
  text-align: center;
  color: #94a3b8;
}

.error {
  color: #ef4444;
}

.providers-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 1.5rem;
}

.provider-card {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 12px;
  overflow: hidden;
}

.provider-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1.25rem;
  border-bottom: 1px solid #334155;
}

.provider-name {
  font-size: 1.125rem;
  font-weight: 600;
}

.provider-badge {
  padding: 0.25rem 0.75rem;
  border-radius: 4px;
  font-size: 0.75rem;
  background: #334155;
  color: #94a3b8;
}

.provider-badge.builtin {
  background: #3b82f6;
  color: white;
}

.provider-badge.worker {
  background: #8b5cf6;
  color: white;
}

.provider-body {
  padding: 1.25rem;
}

.provider-row {
  display: flex;
  justify-content: space-between;
  margin-bottom: 0.75rem;
}

.provider-row:last-child {
  margin-bottom: 0;
}

.provider-label {
  color: #94a3b8;
  font-size: 0.875rem;
}

.provider-value {
  color: #e2e8f0;
  font-size: 0.875rem;
}

.provider-value.online {
  color: #22c55e;
}
</style>
