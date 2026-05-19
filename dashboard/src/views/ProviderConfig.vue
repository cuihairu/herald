<template>
  <div class="config">
    <div class="header">
      <h1 class="title">Provider 配置</h1>
      <button class="back-btn" @click="$router.back()">返回</button>
    </div>

    <div v-if="loading && !config" class="loading">加载中...</div>
    <div v-else-if="error" class="error">{{ error }}</div>
    <div v-else-if="!config" class="empty">Provider 不存在</div>

    <div v-else class="config-container">
      <div class="provider-info">
        <h2 class="provider-name">{{ providerName }}</h2>
        <span class="provider-type">{{ config.type }}</span>
        <span class="provider-status" :class="{ enabled: config.enabled }">
          {{ config.enabled ? '已启用' : '已禁用' }}
        </span>
      </div>

      <form @submit.prevent="handleSave" class="config-form">
        <div class="form-section">
          <h3 class="section-title">配置项</h3>

          <div v-if="Object.keys(schema).length === 0" class="empty-schema">
            此 Provider 暂无可配置项
          </div>

          <div v-for="(fieldType, fieldName) in schema" :key="fieldName" class="form-group">
            <label class="form-label">
              {{ fieldName }}
              <span class="field-type">{{ fieldType }}</span>
            </label>

            <input
              v-if="fieldType === 'string'"
              v-model="formData[fieldName]"
              type="text"
              class="form-input"
              :placeholder="`输入 ${fieldName}`"
            />

            <input
              v-else-if="fieldType === 'number'"
              v-model.number="formData[fieldName]"
              type="number"
              class="form-input"
              :placeholder="`输入 ${fieldName}`"
            />

            <textarea
              v-else
              v-model="formData[fieldName]"
              class="form-textarea"
              rows="3"
              :placeholder="`输入 ${fieldName}`"
            />
          </div>
        </div>

        <div v-if="saveResult" class="result" :class="{ success: saveResult.success, error: !saveResult.success }">
          {{ saveResult.message }}
        </div>

        <div class="form-actions">
          <button type="submit" class="save-btn" :disabled="saving">
            {{ saving ? '保存中...' : '保存配置' }}
          </button>
          <button type="button" class="test-btn" @click="handleTest" :disabled="saving">
            测试连接
          </button>
        </div>
      </form>

      <div class="info-box">
        <h4>说明</h4>
        <ul>
          <li>修改配置后，Provider 将自动重启</li>
          <li>敏感信息（如密码、密钥）将显示为占位符</li>
          <li>配置仅保存在内存中，重启服务后将恢复为配置文件中的值</li>
        </ul>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'

const route = useRoute()

const providerName = ref(route.params.name)
const config = ref(null)
const schema = ref({})
const formData = ref({})
const loading = ref(true)
const saving = ref(false)
const error = ref(null)
const saveResult = ref(null)

async function fetchConfig() {
  loading.value = true
  error.value = null

  try {
    const response = await fetch(`/api/v1/config/${providerName.value}`)
    const data = await response.json()

    if (data.code === 0) {
      config.value = data.data
      schema.value = data.data.schema || {}
      formData.value = { ...data.data.config }
    } else {
      error.value = data.message
    }
  } catch (err) {
    error.value = err.message
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  saveResult.value = null

  try {
    const response = await fetch(`/api/v1/config/${providerName.value}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ config: formData.value })
    })

    const data = await response.json()

    if (data.code === 0) {
      saveResult.value = { success: true, message: '配置保存成功，Provider 已重启' }
    } else {
      saveResult.value = { success: false, message: data.message || '保存失败' }
    }
  } catch (err) {
    saveResult.value = { success: false, message: err.message || '保存失败' }
  } finally {
    saving.value = false
  }
}

async function handleTest() {
  saving.value = true
  saveResult.value = null

  try {
    // Send a test notification
    const response = await fetch('/api/v1/notify', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        title: 'Herald 测试消息',
        body: '这是一条测试消息，用于验证 Provider 配置是否正确',
        level: 'info',
        channels: [providerName.value]
      })
    })

    const data = await response.json()

    if (data.code === 0) {
      saveResult.value = { success: true, message: '测试消息已发送，请检查是否收到' }
    } else {
      saveResult.value = { success: false, message: data.message || '测试失败' }
    }
  } catch (err) {
    saveResult.value = { success: false, message: err.message || '测试失败' }
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  fetchConfig()
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

.back-btn {
  padding: 0.5rem 1rem;
  background: #334155;
  border: none;
  border-radius: 6px;
  color: #e2e8f0;
  cursor: pointer;
  transition: background 0.2s;
}

.back-btn:hover {
  background: #475569;
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

.config-container {
  max-width: 600px;
}

.provider-info {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 1rem;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  margin-bottom: 1.5rem;
}

.provider-name {
  font-size: 1.125rem;
  font-weight: 600;
}

.provider-type {
  padding: 0.25rem 0.75rem;
  background: #3b82f6;
  color: white;
  border-radius: 4px;
  font-size: 0.75rem;
}

.provider-status {
  padding: 0.25rem 0.75rem;
  background: #64748b;
  color: white;
  border-radius: 4px;
  font-size: 0.75rem;
}

.provider-status.enabled {
  background: #22c55e;
}

.config-form {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  padding: 1.5rem;
}

.form-section {
  margin-bottom: 1.5rem;
}

.section-title {
  font-size: 1rem;
  font-weight: 600;
  margin-bottom: 1rem;
  color: #e2e8f0;
}

.empty-schema {
  padding: 1rem;
  text-align: center;
  color: #94a3b8;
  background: #0f172a;
  border-radius: 6px;
}

.form-group {
  margin-bottom: 1rem;
}

.form-label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
  color: #94a3b8;
  font-size: 0.875rem;
}

.field-type {
  padding: 0.125rem 0.375rem;
  background: #334155;
  border-radius: 4px;
  font-size: 0.625rem;
  color: #64748b;
}

.form-input,
.form-textarea {
  width: 100%;
  padding: 0.75rem;
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 6px;
  color: #e2e8f0;
  font-size: 0.875rem;
}

.form-input:focus,
.form-textarea:focus {
  outline: none;
  border-color: #3b82f6;
}

.form-textarea {
  resize: vertical;
  font-family: monospace;
}

.result {
  padding: 0.75rem;
  border-radius: 6px;
  margin-bottom: 1rem;
  font-size: 0.875rem;
}

.result.success {
  background: #059669;
  color: white;
}

.result.error {
  background: #dc2626;
  color: white;
}

.form-actions {
  display: flex;
  gap: 1rem;
}

.save-btn,
.test-btn {
  flex: 1;
  padding: 0.75rem;
  border: none;
  border-radius: 6px;
  font-size: 0.875rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}

.save-btn {
  background: #3b82f6;
  color: white;
}

.save-btn:hover:not(:disabled) {
  background: #2563eb;
}

.test-btn {
  background: #334155;
  color: #e2e8f0;
}

.test-btn:hover:not(:disabled) {
  background: #475569;
}

.save-btn:disabled,
.test-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.info-box {
  padding: 1rem;
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 8px;
  margin-top: 1rem;
}

.info-box h4 {
  font-size: 0.875rem;
  font-weight: 600;
  margin-bottom: 0.5rem;
  color: #e2e8f0;
}

.info-box ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.info-box li {
  font-size: 0.875rem;
  color: #94a3b8;
  padding-left: 1rem;
  position: relative;
  margin-bottom: 0.25rem;
}

.info-box li::before {
  content: '•';
  position: absolute;
  left: 0;
  color: #64748b;
}

@media (max-width: 767px) {
  .header {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .back-btn {
    width: 100%;
  }

  .provider-info {
    flex-direction: column;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .form-actions {
    flex-direction: column;
  }

  .title {
    font-size: 1.25rem;
  }
}
</style>
