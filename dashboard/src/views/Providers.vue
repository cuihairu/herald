<template>
  <div class="providers">
    <h1 class="title">Providers</h1>

    <div v-if="store.loading" class="loading">加载中...</div>

    <div v-else-if="store.error" class="error">{{ store.error }}</div>

    <div v-else class="providers-grid">
      <div v-for="provider in store.providers" :key="provider.name" class="provider-card">
        <div class="provider-header">
          <h2 class="provider-name">{{ getProviderDisplayName(provider.name) }}</h2>
          <span class="provider-badge" :class="provider.type">{{ provider.type }}</span>
        </div>
        <div class="provider-body">
          <div class="provider-row">
            <span class="provider-label">状态:</span>
            <span class="provider-status" :class="getStatusClass(provider)">
              {{ provider.enabled ? '已启用' : '已禁用' }}
            </span>
          </div>
          <div class="provider-row">
            <span class="provider-label">健康:</span>
            <span class="provider-value" :class="{ online: provider.status === 'available' }">
              {{ provider.status === 'available' ? '正常' : '异常' }}
            </span>
          </div>
          <div class="provider-row">
            <span class="provider-label">类型:</span>
            <span class="provider-value">{{ getProviderType(provider.name) }}</span>
          </div>
          <div v-if="provider.since" class="provider-row">
            <span class="provider-label">启动时间:</span>
            <span class="provider-value">{{ formatDate(provider.since) }}</span>
          </div>
        </div>
        <div class="provider-footer">
          <router-link :to="`/providers/${provider.name}/config`" class="config-btn">
            配置
          </router-link>
          <button
            class="toggle-btn"
            :class="{ enabled: provider.enabled }"
            @click="toggleProvider(provider)"
            :disabled="store.loading"
          >
            {{ provider.enabled ? '禁用' : '启用' }}
          </button>
        </div>
      </div>
    </div>

    <div v-if="!store.loading && store.providers.length === 0" class="empty">
      暂无 Providers
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

function getProviderDisplayName(name) {
  const displayNames = {
    'log': '日志',
    'telegram': 'Telegram',
    'feishu': '飞书',
    'wecom': '企业微信',
    'email': '邮件',
    'webhook': 'Webhook',
    'discord': 'Discord',
    'slack': 'Slack',
    'dingtalk': '钉钉',
    'aliyunsms': '阿里云短信',
    'tencentsms': '腾讯云短信',
    'neteasesms': '网易云信短信'
  }
  return displayNames[name] || name
}

function getProviderType(name) {
  if (name.includes('sms')) {
    return '短信'
  }
  if (name === 'email') {
    return '邮件'
  }
  if (name === 'webhook') {
    return 'Webhook'
  }
  return '即时通讯'
}

function getStatusClass(provider) {
  if (!provider.enabled) {
    return 'disabled'
  }
  if (provider.status === 'available') {
    return 'enabled'
  }
  return 'error'
}

async function toggleProvider(provider) {
  try {
    if (provider.enabled) {
      await store.disableProvider(provider.name)
    } else {
      await store.enableProvider(provider.name)
    }
  } catch (err) {
    console.error('Failed to toggle provider:', err)
  }
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
  transition: border-color 0.2s;
}

.provider-card:hover {
  border-color: #475569;
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

.provider-status {
  padding: 0.125rem 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
}

.provider-status.enabled {
  background: #22c55e;
  color: white;
}

.provider-status.disabled {
  background: #6b7280;
  color: white;
}

.provider-status.error {
  background: #ef4444;
  color: white;
}

.provider-footer {
  padding: 1rem 1.25rem;
  border-top: 1px solid #334155;
  display: flex;
  gap: 0.5rem;
}

.config-btn {
  flex: 1;
  padding: 0.625rem;
  border: 1px solid #334155;
  border-radius: 6px;
  background: #1e293b;
  color: #94a3b8;
  font-size: 0.875rem;
  text-align: center;
  text-decoration: none;
  transition: all 0.2s;
}

.config-btn:hover {
  background: #334155;
  color: #e2e8f0;
}

.toggle-btn {
  flex: 1;
  padding: 0.625rem;
  border: 1px solid #334155;
  border-radius: 6px;
  background: #1e293b;
  color: #94a3b8;
  font-size: 0.875rem;
  cursor: pointer;
  transition: all 0.2s;
}

.toggle-btn:hover:not(:disabled) {
  background: #334155;
  color: #e2e8f0;
}

.toggle-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.toggle-btn.enabled {
  background: #dc2626;
  border-color: #dc2626;
  color: white;
}

.toggle-btn.enabled:hover:not(:disabled) {
  background: #b91c1c;
  border-color: #b91c1c;
}

.empty {
  padding: 3rem;
  text-align: center;
  color: #94a3b8;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
}

@media (max-width: 767px) {
  .providers-grid {
    grid-template-columns: 1fr;
    gap: 1rem;
  }

  .provider-header {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.5rem;
    padding: 1rem;
  }

  .provider-body {
    padding: 1rem;
  }

  .provider-footer {
    flex-direction: column;
    padding: 0.75rem 1rem;
  }

  .title {
    font-size: 1.25rem;
  }
}
</style>
