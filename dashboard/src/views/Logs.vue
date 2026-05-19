<template>
  <div class="logs">
    <div class="header">
      <h1 class="title">任务日志</h1>
      <button class="refresh-btn" @click="refresh" :disabled="store.loading">
        {{ store.loading ? '加载中...' : '刷新' }}
      </button>
    </div>

    <div class="filters">
      <div class="filter-group">
        <label class="filter-label">状态</label>
        <select v-model="filter.status" class="filter-select" @change="fetchLogs">
          <option value="">全部</option>
          <option value="success">成功</option>
          <option value="failed">失败</option>
          <option value="pending">进行中</option>
        </select>
      </div>

      <div class="filter-group">
        <label class="filter-label">Provider</label>
        <select v-model="filter.provider" class="filter-select" @change="fetchLogs">
          <option value="">全部</option>
          <option v-for="p in providers" :key="p" :value="p">{{ p }}</option>
        </select>
      </div>

      <div class="filter-group">
        <label class="filter-label">级别</label>
        <select v-model="filter.level" class="filter-select" @change="fetchLogs">
          <option value="">全部</option>
          <option value="info">信息</option>
          <option value="warning">警告</option>
          <option value="error">错误</option>
          <option value="critical">严重</option>
        </select>
      </div>

      <div class="filter-group">
        <label class="filter-label">时间范围</label>
        <select v-model="timeRange" class="filter-select" @change="onTimeRangeChange">
          <option value="1h">最近1小时</option>
          <option value="24h">最近24小时</option>
          <option value="7d">最近7天</option>
          <option value="30d">最近30天</option>
          <option value="all">全部</option>
        </select>
      </div>
    </div>

    <div class="stats" v-if="stats">
      <div class="stat-item">
        <span class="stat-label">总数:</span>
        <span class="stat-value">{{ stats.total }}</span>
      </div>
      <div class="stat-item">
        <span class="stat-label">成功:</span>
        <span class="stat-value success">{{ stats.by_status?.success || 0 }}</span>
      </div>
      <div class="stat-item">
        <span class="stat-label">失败:</span>
        <span class="stat-value error">{{ stats.by_status?.failed || 0 }}</span>
      </div>
      <div class="stat-item">
        <span class="stat-label">进行中:</span>
        <span class="stat-value pending">{{ stats.by_status?.pending || 0 }}</span>
      </div>
    </div>

    <div v-if="store.loading && logs.length === 0" class="loading">加载中...</div>
    <div v-else-if="store.error" class="error">{{ store.error }}</div>
    <div v-else-if="logs.length === 0" class="empty">暂无日志</div>

    <div v-else class="logs-list">
      <div v-for="log in logs" :key="log.id" class="log-item" :class="log.status">
        <div class="log-header">
          <div class="log-provider">{{ log.provider }}</div>
          <div class="log-status" :class="log.status">{{ getStatusText(log.status) }}</div>
          <div class="log-time">{{ formatTime(log.created_at) }}</div>
        </div>
        <div class="log-body">
          <div class="log-title">{{ log.title }}</div>
          <div v-if="log.body" class="log-message">{{ log.body }}</div>
          <div v-if="log.error" class="log-error">{{ log.error }}</div>
          <div v-if="log.level" class="log-level">
            <span class="level-badge" :class="log.level">{{ log.level }}</span>
          </div>
          <div v-if="log.duration !== undefined" class="log-duration">
            耗时: {{ log.duration }}ms
          </div>
        </div>
      </div>
    </div>

    <div v-if="total > limit" class="pagination">
      <button
        class="page-btn"
        :disabled="offset === 0"
        @click="prevPage"
      >
        上一页
      </button>
      <span class="page-info">
        {{ offset + 1 }} - {{ Math.min(offset + limit, total) }} / {{ total }}
      </span>
      <button
        class="page-btn"
        :disabled="offset + limit >= total"
        @click="nextPage"
      >
        下一页
      </button>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useHeraldStore } from '../stores/herald'

const store = useHeraldStore()

const logs = ref([])
const stats = ref(null)
const total = ref(0)
const offset = ref(0)
const limit = ref(50)

const filter = ref({
  status: '',
  provider: '',
  level: '',
  since: '',
  until: ''
})

const timeRange = ref('24h')
const providers = ref([])

let refreshInterval = null

async function fetchLogs() {
  store.loading = true
  store.error = null

  try {
    const params = new URLSearchParams({
      offset: offset.value,
      limit: limit.value,
      ...Object.fromEntries(
        Object.entries(filter.value).filter(([_, v]) => v !== '')
      )
    })

    const response = await fetch(`/api/v1/logs?${params}`)
    const data = await response.json()

    if (data.code === 0) {
      logs.value = data.data.logs
      total.value = data.data.total
    } else {
      store.error = data.message
    }
  } catch (err) {
    store.error = err.message
  } finally {
    store.loading = false
  }
}

async function fetchStats() {
  try {
    const response = await fetch('/api/v1/logs/stats')
    const data = await response.json()

    if (data.code === 0) {
      stats.value = data.data
    }
  } catch (err) {
    console.error('Failed to fetch stats:', err)
  }
}

async function fetchProviders() {
  await store.fetchProviders()
  providers.value = [...new Set(store.providers.map(p => p.name))]
}

function onTimeRangeChange() {
  const now = new Date()
  let since = null

  switch (timeRange.value) {
    case '1h':
      since = new Date(now.getTime() - 60 * 60 * 1000)
      break
    case '24h':
      since = new Date(now.getTime() - 24 * 60 * 60 * 1000)
      break
    case '7d':
      since = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000)
      break
    case '30d':
      since = new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000)
      break
    case 'all':
      since = null
      break
  }

  if (since) {
    filter.value.since = since.toISOString()
  } else {
    filter.value.since = ''
  }
  filter.value.until = ''

  fetchLogs()
}

function refresh() {
  fetchLogs()
  fetchStats()
}

function prevPage() {
  offset.value = Math.max(0, offset.value - limit.value)
  fetchLogs()
}

function nextPage() {
  if (offset.value + limit.value < total.value) {
    offset.value += limit.value
    fetchLogs()
  }
}

function formatTime(timestamp) {
  if (!timestamp) return '-'
  const date = new Date(timestamp)
  return date.toLocaleString('zh-CN')
}

function getStatusText(status) {
  const map = {
    success: '成功',
    failed: '失败',
    pending: '进行中'
  }
  return map[status] || status
}

onMounted(async () => {
  await fetchProviders()
  await fetchStats()
  onTimeRangeChange()
  fetchLogs()

  refreshInterval = setInterval(refresh, 10000)
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
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1.5rem;
}

.refresh-btn {
  padding: 0.5rem 1rem;
  background: #3b82f6;
  border: none;
  border-radius: 6px;
  color: white;
  cursor: pointer;
  transition: background 0.2s;
}

.refresh-btn:hover:not(:disabled) {
  background: #2563eb;
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
  margin-bottom: 1.5rem;
  padding: 1rem;
  background: #1e293b;
  border-radius: 8px;
}

.filter-group {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.filter-label {
  font-size: 0.75rem;
  color: #94a3b8;
}

.filter-select {
  padding: 0.5rem;
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 4px;
  color: #e2e8f0;
  font-size: 0.875rem;
  min-width: 120px;
}

.stats {
  display: flex;
  gap: 1.5rem;
  margin-bottom: 1rem;
  padding: 1rem;
  background: #1e293b;
  border-radius: 8px;
}

.stat-item {
  display: flex;
  gap: 0.5rem;
}

.stat-label {
  color: #94a3b8;
  font-size: 0.875rem;
}

.stat-value {
  font-weight: 600;
  color: #e2e8f0;
}

.stat-value.success {
  color: #22c55e;
}

.stat-value.error {
  color: #ef4444;
}

.stat-value.pending {
  color: #f59e0b;
}

.loading, .error, .empty {
  padding: 3rem;
  text-align: center;
  color: #94a3b8;
  background: #1e293b;
  border-radius: 8px;
}

.error {
  color: #ef4444;
}

.logs-list {
  display: grid;
  gap: 1rem;
}

.log-item {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  overflow: hidden;
}

.log-item.success {
  border-left: 4px solid #22c55e;
}

.log-item.failed {
  border-left: 4px solid #ef4444;
}

.log-item.pending {
  border-left: 4px solid #f59e0b;
}

.log-header {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
  border-bottom: 1px solid #334155;
}

.log-provider {
  font-weight: 600;
  color: #e2e8f0;
}

.log-status {
  padding: 0.125rem 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
}

.log-status.success {
  background: #22c55e;
  color: white;
}

.log-status.failed {
  background: #ef4444;
  color: white;
}

.log-status.pending {
  background: #f59e0b;
  color: white;
}

.log-time {
  margin-left: auto;
  font-size: 0.75rem;
  color: #94a3b8;
}

.log-body {
  padding: 1rem;
  display: grid;
  gap: 0.5rem;
}

.log-title {
  font-weight: 500;
  color: #e2e8f0;
}

.log-message {
  color: #94a3b8;
  font-size: 0.875rem;
  white-space: pre-wrap;
}

.log-error {
  color: #ef4444;
  font-size: 0.875rem;
}

.level-badge {
  display: inline-block;
  padding: 0.125rem 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
  background: #334155;
  color: #94a3b8;
}

.level-badge.info {
  background: #3b82f6;
  color: white;
}

.level-badge.warning {
  background: #f59e0b;
  color: white;
}

.level-badge.error,
.level-badge.critical {
  background: #ef4444;
  color: white;
}

.log-duration {
  font-size: 0.75rem;
  color: #64748b;
}

.pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 1rem;
  margin-top: 1.5rem;
}

.page-btn {
  padding: 0.5rem 1rem;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 6px;
  color: #e2e8f0;
  cursor: pointer;
  transition: all 0.2s;
}

.page-btn:hover:not(:disabled) {
  background: #334155;
}

.page-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.page-info {
  color: #94a3b8;
  font-size: 0.875rem;
}

@media (max-width: 767px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
    gap: 1rem;
  }

  .refresh-btn {
    width: 100%;
  }

  .filters {
    flex-direction: column;
    gap: 0.75rem;
  }

  .filter-group {
    flex: 1 1 100%;
  }

  .filter-select {
    width: 100%;
    min-width: auto;
  }

  .stats {
    flex-wrap: wrap;
    gap: 1rem;
  }

  .log-header {
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .log-time {
    margin-left: 0;
    font-size: 0.625rem;
  }

  .pagination {
    flex-direction: column;
    gap: 0.75rem;
  }

  .title {
    font-size: 1.25rem;
  }
}

@media (max-width: 480px) {
  .stats {
    grid-template-columns: 1fr 1fr;
    gap: 0.75rem;
  }

  .stat-item {
    flex-direction: column;
    gap: 0.25rem;
  }
}
</style>
