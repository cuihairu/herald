<template>
  <div class="app">
    <nav class="nav">
      <div class="nav-brand">Herald</div>
      <button class="menu-toggle" @click="menuOpen = !menuOpen" :class="{ active: menuOpen }">
        <span></span>
        <span></span>
        <span></span>
      </button>
      <div class="nav-links" :class="{ open: menuOpen }">
        <router-link to="/" class="nav-link" @click="menuOpen = false">仪表盘</router-link>
        <router-link to="/providers" class="nav-link" @click="menuOpen = false">Providers</router-link>
        <router-link to="/workers" class="nav-link" @click="menuOpen = false">Workers</router-link>
        <router-link to="/logs" class="nav-link" @click="menuOpen = false">日志</router-link>
        <router-link to="/send" class="nav-link" @click="menuOpen = false">发送消息</router-link>
      </div>
    </nav>
    <main class="main">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import { ref } from 'vue'

const menuOpen = ref(false)
</script>

<style scoped>
.nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 1rem;
  height: 60px;
  background: #1e293b;
  border-bottom: 1px solid #334155;
  position: sticky;
  top: 0;
  z-index: 100;
}

.nav-brand {
  font-size: 1.25rem;
  font-weight: 600;
  color: #3b82f6;
}

.menu-toggle {
  display: none;
  flex-direction: column;
  gap: 5px;
  background: none;
  border: none;
  cursor: pointer;
  padding: 5px;
}

.menu-toggle span {
  width: 24px;
  height: 2px;
  background: #94a3b8;
  border-radius: 2px;
  transition: all 0.3s;
}

.menu-toggle.active span:nth-child(1) {
  transform: rotate(45deg) translate(5px, 5px);
}

.menu-toggle.active span:nth-child(2) {
  opacity: 0;
}

.menu-toggle.active span:nth-child(3) {
  transform: rotate(-45deg) translate(5px, -5px);
}

.nav-links {
  display: flex;
  gap: 1.5rem;
}

.nav-link {
  color: #94a3b8;
  text-decoration: none;
  transition: color 0.2s;
  padding: 0.5rem;
  border-radius: 4px;
}

.nav-link:hover,
.nav-link.router-link-active {
  color: #3b82f6;
  background: rgba(59, 130, 246, 0.1);
}

.main {
  padding: 1rem;
  max-width: 1200px;
  margin: 0 auto;
}

/* Tablet */
@media (min-width: 768px) {
  .nav {
    padding: 0 2rem;
  }

  .nav-links {
    gap: 2rem;
  }

  .main {
    padding: 2rem;
  }
}

/* Desktop */
@media (min-width: 1024px) {
  .nav-links {
    gap: 2rem;
  }
}

/* Mobile */
@media (max-width: 767px) {
  .menu-toggle {
    display: flex;
  }

  .nav-links {
    position: fixed;
    top: 60px;
    left: 0;
    right: 0;
    flex-direction: column;
    gap: 0;
    background: #1e293b;
    border-bottom: 1px solid #334155;
    padding: 1rem;
    transform: translateY(-100%);
    opacity: 0;
    transition: all 0.3s ease;
    pointer-events: none;
  }

  .nav-links.open {
    transform: translateY(0);
    opacity: 1;
    pointer-events: auto;
  }

  .nav-link {
    padding: 0.75rem 1rem;
    width: 100%;
    text-align: center;
  }

  .main {
    padding: 1rem;
  }
}
</style>
