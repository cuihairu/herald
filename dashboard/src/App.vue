<template>
  <div class="app">
    <nav class="nav">
      <div class="nav-brand">Herald</div>
      <div class="nav-links">
        <router-link to="/" class="nav-link">仪表盘</router-link>
        <router-link to="/providers" class="nav-link">Providers</router-link>
        <router-link to="/workers" class="nav-link">Workers</router-link>
        <router-link to="/send" class="nav-link">发送消息</router-link>
      </div>
    </nav>
    <main class="main">
      <router-view />
    </main>
  </div>
</template>

<script setup>
</script>

<style scoped>
.nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 2rem;
  height: 60px;
  background: #1e293b;
  border-bottom: 1px solid #334155;
}

.nav-brand {
  font-size: 1.25rem;
  font-weight: 600;
  color: #3b82f6;
}

.nav-links {
  display: flex;
  gap: 2rem;
}

.nav-link {
  color: #94a3b8;
  text-decoration: none;
  transition: color 0.2s;
}

.nav-link:hover,
.nav-link.router-link-active {
  color: #3b82f6;
}

.main {
  padding: 2rem;
  max-width: 1200px;
  margin: 0 auto;
}
</style>
