<template>
  <el-container v-if="user" class="app-shell">
    <el-header class="app-header">
      <div class="brand">
        <img src="/logo.svg" alt="logo" class="logo" />
        <span class="title">message-hub</span>
        <el-tag size="small" type="info" effect="plain">v{{ version }}</el-tag>
      </div>
      <el-dropdown @command="onUserCommand">
        <span class="user-entry">
          {{ user.display_name || user.email }}
          <el-tag size="small" :type="user.role === 'admin' ? 'danger' : user.role === 'tenant_admin' ? 'warning' : 'info'">
            {{ roleText[user.role] }}
          </el-tag>
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item disabled>租户：{{ user.tenant?.name || '—' }}</el-dropdown-item>
            <el-dropdown-item command="logout" divided>退出登录</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </el-header>
    <el-container>
      <el-aside width="180px" class="app-aside">
        <el-menu :default-active="view" @select="(v) => (view = v)">
          <el-menu-item index="messages">消息中心</el-menu-item>
          <el-menu-item index="tokens">推送令牌</el-menu-item>
          <el-menu-item v-if="user.role !== 'user'" index="admin">管理</el-menu-item>
        </el-menu>
      </el-aside>
      <el-main>
        <Messages v-if="view === 'messages'" />
        <Tokens v-else-if="view === 'tokens'" />
        <Admin v-else :user="user" />
      </el-main>
    </el-container>
  </el-container>
  <Login v-else @done="loadMe" />
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, roleText } from './api'
import Login from './views/Login.vue'
import Messages from './views/Messages.vue'
import Tokens from './views/Tokens.vue'
import Admin from './views/Admin.vue'

const user = ref(null)
const view = ref('messages')
const version = ref('')

const onAuthExpired = () => {
  user.value = null
}

async function loadMe() {
  try {
    const [me, cfg] = await Promise.all([api.get('/api/v1/auth/me'), api.get('/api/v1/config')])
    user.value = me
    version.value = cfg.version
  } catch {
    user.value = null
  }
}

async function onUserCommand(cmd) {
  if (cmd !== 'logout') return
  try {
    await api.post('/api/v1/auth/logout')
  } catch {
    // 登出失败不阻塞本地登出
  }
  user.value = null
  ElMessage.success('已退出登录')
}

onMounted(() => {
  window.addEventListener('auth-expired', onAuthExpired)
  loadMe()
})
onUnmounted(() => window.removeEventListener('auth-expired', onAuthExpired))
</script>

<style>
html,
body,
#app {
  height: 100%;
  margin: 0;
}
#app {
  font-family: 'Helvetica Neue', Arial, 'PingFang SC', 'Microsoft YaHei', sans-serif;
}
.app-shell {
  height: 100%;
}
.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--el-border-color-light);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
}
.brand .logo {
  width: 28px;
  height: 28px;
}
.brand .title {
  font-size: 18px;
  font-weight: 600;
}
.user-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  color: var(--el-text-color-primary);
  outline: none;
}
.app-aside {
  border-right: 1px solid var(--el-border-color-light);
}
.app-aside .el-menu {
  border-right: none;
}
</style>
