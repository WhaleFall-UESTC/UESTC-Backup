<script setup lang="ts">
import { computed, ref } from 'vue'
import HomePage from './pages/HomePage.vue'
import RestorePage from './pages/RestorePage.vue'
import HistoryPage from './pages/HistoryPage.vue'
import SettingsPage from './pages/SettingsPage.vue'
import AboutPage from './pages/AboutPage.vue'

const navs = [
  { key: 'home', label: '首页', page: HomePage },
  { key: 'restore', label: '还原', page: RestorePage },
  { key: 'history', label: '记录', page: HistoryPage },
  { key: 'settings', label: '设置', page: SettingsPage },
  { key: 'about', label: '关于', page: AboutPage },
] as const

type PageKey = (typeof navs)[number]['key']
const current = ref<PageKey>('home')
const active = computed(() => navs.find((n) => n.key === current.value)!)
</script>

<template>
  <div class="app">
    <nav class="topbar">
      <span class="brand">UESTC-Backup</span>
      <button
        v-for="n in navs"
        :key="n.key"
        class="nav-item"
        :class="{ active: current === n.key }"
        @click="current = n.key"
      >
        {{ n.label }}
      </button>
    </nav>
    <main class="content">
      <component :is="active.page" />
    </main>
    <footer class="statusbar">空闲</footer>
  </div>
</template>
