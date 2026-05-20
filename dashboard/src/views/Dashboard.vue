<template>
  <div class="dashboard">
    <h1 class="title">仪表盘</h1>

    <div v-if="store.loading" class="loading">加载中...</div>

    <div v-else-if="store.error" class="error">{{ store.error }}</div>

    <div v-else class="stats">
      <div class="stat-card">
        <div class="stat-label">系统状态</div>
        <div class="stat-value" :class="{ online: store.status?.status === 'running' }">
          {{ store.status?.status || 'unknown' }}
        </div>
      </div>

      <div class="stat-card">
        <div class="stat-label">Provider 数量</div>
        <div class="stat-value">{{ store.providers.length }}</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">在线 Provider</div>
        <div class="stat-value">{{ onlineCount }}</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">通知总数</div>
        <div class="stat-value">{{ store.logStats?.total || 0 }}</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">成功</div>
        <div class="stat-value success">{{ store.logStats?.by_status?.success || 0 }}</div>
      </div>

      <div class="stat-card">
        <div class="stat-label">失败</div>
        <div class="stat-value failed">{{ store.logStats?.by_status?.failed || 0 }}</div>
      </div>
    </div>

    <div class="providers-section">
      <h2 class="section-title">Providers</h2>
      <div class="providers-list">
        <div v-for="provider in store.providers" :key="provider.name" class="provider-item">
          <div class="provider-name">{{ provider.name }}</div>
          <div class="provider-type">{{ provider.type }}</div>
          <div class="provider-status" :class="{ available: provider.status === 'available' }">
            {{ provider.status }}
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted } from 'vue'
import { useHeraldStore } from '../stores/herald'

const store = useHeraldStore()

const onlineCount = computed(() => {
  return store.providers.filter(p => p.status === 'available').length
})

onMounted(async () => {
  await Promise.all([
    store.fetchStatus(),
    store.fetchProviders(),
    store.fetchLogStats()
  ])
})
</script>

<style scoped>
.title {
  font-size: 1.25rem;
  font-weight: 600;
  margin-bottom: 1.5rem;
}

.loading, .error {
  padding: 2rem;
  text-align: center;
  color: #94a3b8;
}

.error {
  color: #ef4444;
}

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 0.75rem;
  margin-bottom: 1.5rem;
}

.stat-card {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  padding: 1rem;
}

.stat-label {
  color: #94a3b8;
  font-size: 0.75rem;
  margin-bottom: 0.25rem;
}

.stat-value {
  font-size: 1.25rem;
  font-weight: 600;
  color: #e2e8f0;
}

.stat-value.online {
  color: #22c55e;
}

.stat-value.success {
  color: #22c55e;
}

.stat-value.failed {
  color: #ef4444;
}

.section-title {
  font-size: 1rem;
  font-weight: 600;
  margin-bottom: 0.75rem;
}

.providers-list {
  display: grid;
  gap: 0.5rem;
}

.provider-item {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
}

.provider-name {
  flex: 1;
  font-weight: 500;
  font-size: 0.875rem;
}

.provider-type {
  color: #94a3b8;
  font-size: 0.75rem;
}

.provider-status {
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  font-size: 0.625rem;
  background: #ef4444;
  color: white;
}

.provider-status.available {
  background: #22c55e;
}

@media (min-width: 768px) {
  .title {
    font-size: 1.5rem;
    margin-bottom: 2rem;
  }

  .stats {
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 1rem;
    margin-bottom: 2rem;
  }

  .stat-card {
    padding: 1.5rem;
  }

  .stat-label {
    font-size: 0.875rem;
  }

  .stat-value {
    font-size: 1.5rem;
  }

  .section-title {
    font-size: 1.125rem;
    margin-bottom: 1rem;
  }

  .provider-item {
    gap: 1rem;
    padding: 1rem;
  }

  .provider-name {
    font-size: 1rem;
  }

  .provider-type {
    font-size: 0.875rem;
  }

  .provider-status {
    font-size: 0.75rem;
    padding: 0.25rem 0.75rem;
  }
}
</style>
