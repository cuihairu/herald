<template>
  <div class="workers">
    <h1 class="title">Workers</h1>

    <div v-if="store.loading" class="loading">加载中...</div>

    <div v-else-if="store.error" class="error">{{ store.error }}</div>

    <div v-else>
      <div class="stats">
        <div class="stat-card">
          <div class="stat-label">连接数量</div>
          <div class="stat-value">{{ store.workers.length }}</div>
        </div>

        <div class="stat-card">
          <div class="stat-label">队列大小</div>
          <div class="stat-value">{{ store.queue.size }}</div>
        </div>
      </div>

      <div class="workers-section">
        <h2 class="section-title">已连接 Workers</h2>

        <div v-if="store.workers.length === 0" class="empty">
          暂无连接的 Workers
        </div>

        <div v-else class="workers-list">
          <div v-for="worker in store.workers" :key="worker.worker_id" class="worker-item">
            <div class="worker-header">
              <div class="worker-id">{{ worker.worker_id }}</div>
              <div class="worker-badge">{{ worker.platform }}</div>
            </div>

            <div class="worker-details">
              <div class="detail-row">
                <span class="label">版本:</span>
                <span class="value">{{ worker.version }}</span>
              </div>

              <div class="detail-row">
                <span class="label">能力:</span>
                <div class="capabilities">
                  <span v-for="cap in worker.capabilities" :key="cap" class="cap-badge">
                    {{ cap }}
                  </span>
                </div>
              </div>

              <div class="detail-row">
                <span class="label">连接时间:</span>
                <span class="value">{{ formatTime(worker.connected_at) }}</span>
              </div>

              <div class="detail-row">
                <span class="label">最后心跳:</span>
                <span class="value" :class="{ stale: isStale(worker.last_heartbeat) }">
                  {{ formatTime(worker.last_heartbeat) }}
                </span>
              </div>

              <div v-if="worker.status && Object.keys(worker.status).length > 0" class="detail-row">
                <span class="label">状态:</span>
                <div class="status-map">
                  <span v-for="(v, k) in worker.status" :key="k" class="status-item">
                    {{ k }}: {{ v }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted } from 'vue'
import { useHeraldStore } from '../stores/herald'

const store = useHeraldStore()

let refreshInterval = null

async function refresh() {
  await Promise.all([
    store.fetchWorkers(),
    store.fetchQueue()
  ])
}

function formatTime(timestamp) {
  if (!timestamp) return '-'
  const date = new Date(timestamp)
  return date.toLocaleString('zh-CN')
}

function isStale(lastHeartbeat) {
  if (!lastHeartbeat) return false
  const now = Date.now()
  const last = new Date(lastHeartbeat).getTime()
  return now - last > 60000 // 1 minute
}

onMounted(async () => {
  await refresh()
  refreshInterval = setInterval(refresh, 5000) // Refresh every 5 seconds
})

onUnmounted(() => {
  if (refreshInterval) {
    clearInterval(refreshInterval)
  }
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

.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 1rem;
  margin-bottom: 2rem;
}

.stat-card {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  padding: 1.5rem;
}

.stat-label {
  color: #94a3b8;
  font-size: 0.875rem;
  margin-bottom: 0.5rem;
}

.stat-value {
  font-size: 1.5rem;
  font-weight: 600;
  color: #e2e8f0;
}

.section-title {
  font-size: 1.125rem;
  font-weight: 600;
  margin-bottom: 1rem;
}

.empty {
  padding: 3rem;
  text-align: center;
  color: #94a3b8;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
}

.workers-list {
  display: grid;
  gap: 1rem;
}

.worker-item {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  padding: 1rem;
}

.worker-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.worker-id {
  font-weight: 600;
  font-size: 1.125rem;
}

.worker-badge {
  padding: 0.25rem 0.75rem;
  background: #3b82f6;
  color: white;
  border-radius: 4px;
  font-size: 0.75rem;
}

.worker-details {
  display: grid;
  gap: 0.5rem;
}

.detail-row {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
}

.label {
  color: #94a3b8;
  font-size: 0.875rem;
  min-width: 80px;
}

.value {
  color: #e2e8f0;
  font-size: 0.875rem;
}

.value.stale {
  color: #ef4444;
}

.capabilities {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}

.cap-badge {
  padding: 0.125rem 0.5rem;
  background: #334155;
  border-radius: 4px;
  font-size: 0.75rem;
  color: #94a3b8;
}

.status-map {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.status-item {
  padding: 0.125rem 0.5rem;
  background: #334155;
  border-radius: 4px;
  font-size: 0.75rem;
  color: #94a3b8;
}
</style>
