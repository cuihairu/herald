<template>
  <div class="send">
    <h1 class="title">发送消息</h1>

    <div class="form-container">
      <form @submit.prevent="handleSubmit" class="form">
        <div class="form-group">
          <label class="form-label">标题</label>
          <input v-model="form.title" type="text" class="form-input" placeholder="输入标题" required />
        </div>

        <div class="form-group">
          <label class="form-label">内容</label>
          <textarea v-model="form.body" class="form-textarea" rows="4" placeholder="输入内容"></textarea>
        </div>

        <div class="form-group">
          <label class="form-label">级别</label>
          <select v-model="form.level" class="form-select">
            <option value="">默认</option>
            <option value="info">信息</option>
            <option value="warning">警告</option>
            <option value="error">错误</option>
          </select>
        </div>

        <div class="form-group">
          <label class="form-label">Channels</label>
          <div class="checkbox-group">
            <label v-for="provider in providers" :key="provider.name" class="checkbox">
              <input type="checkbox" v-model="form.channels" :value="provider.name" />
              <span>{{ provider.name }}</span>
            </label>
          </div>
        </div>

        <div v-if="result" class="result" :class="{ success: result.success, error: !result.success }">
          {{ result.message }}
        </div>

        <button type="submit" class="btn" :disabled="store.loading">
          {{ store.loading ? '发送中...' : '发送' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { useHeraldStore } from '../stores/herald'

const store = useHeraldStore()
const providers = ref([])
const result = ref(null)

const form = reactive({
  title: '',
  body: '',
  level: '',
  channels: []
})

async function handleSubmit() {
  result.value = null

  try {
    await store.sendNotify({
      title: form.title,
      body: form.body,
      level: form.level,
      channels: form.channels
    })
    result.value = { success: true, message: '发送成功' }
  } catch (err) {
    result.value = { success: false, message: err.message || '发送失败' }
  }
}

onMounted(async () => {
  await store.fetchProviders()
  providers.value = store.providers
})
</script>

<style scoped>
.title {
  font-size: 1.5rem;
  font-weight: 600;
  margin-bottom: 2rem;
}

.form-container {
  max-width: 600px;
}

.form {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 12px;
  padding: 1.5rem;
}

.form-group {
  margin-bottom: 1.5rem;
}

.form-group:last-child {
  margin-bottom: 0;
}

.form-label {
  display: block;
  margin-bottom: 0.5rem;
  color: #94a3b8;
  font-size: 0.875rem;
}

.form-input,
.form-select,
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
.form-select:focus,
.form-textarea:focus {
  outline: none;
  border-color: #3b82f6;
}

.form-textarea {
  resize: vertical;
  font-family: monospace;
}

.checkbox-group {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
}

.checkbox {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  cursor: pointer;
}

.checkbox input {
  accent-color: #3b82f6;
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

.btn {
  width: 100%;
  padding: 0.75rem;
  background: #3b82f6;
  border: none;
  border-radius: 6px;
  color: white;
  font-size: 0.875rem;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}

.btn:hover:not(:disabled) {
  background: #2563eb;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

@media (max-width: 767px) {
  .form-container {
    max-width: 100%;
  }

  .checkbox-group {
    flex-direction: column;
    gap: 0.5rem;
  }

  .checkbox {
    justify-content: flex-start;
  }

  .title {
    font-size: 1.25rem;
  }
}
</style>
