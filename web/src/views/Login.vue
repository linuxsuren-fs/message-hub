<template>
  <div class="login-wrap">
    <el-card class="login-card">
      <div class="login-head">
        <img src="/logo.svg" alt="logo" />
        <h2>message-hub</h2>
        <p class="sub">消息接收与转发中间件</p>
      </div>
      <el-tabs v-model="tab">
        <el-tab-pane label="登录" name="login">
          <el-form @submit.prevent="doLogin">
            <el-form-item>
              <el-input v-model="loginForm.email" placeholder="邮箱" size="large" />
            </el-form-item>
            <el-form-item>
              <el-input v-model="loginForm.password" type="password" placeholder="密码" show-password size="large" @keyup.enter="doLogin" />
            </el-form-item>
            <el-button type="primary" size="large" class="submit" :loading="loading" @click="doLogin">登 录</el-button>
          </el-form>
        </el-tab-pane>
        <el-tab-pane v-if="registrationOpen" label="注册" name="register">
          <el-form @submit.prevent="doRegister">
            <el-form-item>
              <el-input v-model="regForm.email" placeholder="邮箱" size="large" />
            </el-form-item>
            <el-form-item>
              <div class="code-row">
                <el-input v-model="regForm.code" placeholder="邮箱验证码" size="large" maxlength="6" @keyup.enter="doRegister" />
                <el-button size="large" :disabled="cooldown > 0" :loading="sending" @click="sendCode">
                  {{ cooldown > 0 ? `${cooldown}s` : '发送验证码' }}
                </el-button>
              </div>
            </el-form-item>
            <el-form-item>
              <el-input v-model="regForm.password" type="password" placeholder="密码（至少 8 位）" show-password size="large" @keyup.enter="doRegister" />
            </el-form-item>
            <el-button type="primary" size="large" class="submit" :loading="loading" @click="doRegister">注 册</el-button>
          </el-form>
        </el-tab-pane>
      </el-tabs>
      <div v-if="oauthProviders.length" class="oauth">
        <el-divider>或使用第三方登录</el-divider>
        <el-button v-for="p in oauthProviders" :key="p" @click="oauth(p)">{{ providerText[p] || p }}</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../api'

const emit = defineEmits(['done'])
const tab = ref('login')
const loading = ref(false)
const sending = ref(false)
const cooldown = ref(0)
const oauthProviders = ref([])
const registrationOpen = ref(true)

const loginForm = reactive({ email: '', password: '' })
const regForm = reactive({ email: '', password: '', code: '' })
const providerText = { github: 'GitHub', gitee: 'Gitee' }

let timer = null

function startCooldown() {
  cooldown.value = 60
  timer = setInterval(() => {
    cooldown.value -= 1
    if (cooldown.value <= 0) clearInterval(timer)
  }, 1000)
}

async function doLogin() {
  if (!loginForm.email || !loginForm.password) {
    ElMessage.warning('请输入邮箱和密码')
    return
  }
  loading.value = true
  try {
    await api.post('/api/v1/auth/login', { ...loginForm })
    ElMessage.success('登录成功')
    emit('done')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function sendCode() {
  if (!regForm.email) {
    ElMessage.warning('请先输入邮箱')
    return
  }
  sending.value = true
  try {
    await api.post('/api/v1/auth/verification-code', { email: regForm.email })
    ElMessage.success('验证码已发送，请查收邮箱')
    startCooldown()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    sending.value = false
  }
}

async function doRegister() {
  if (!regForm.email || !regForm.password) {
    ElMessage.warning('请完整填写邮箱和密码')
    return
  }
  loading.value = true
  try {
    await api.post('/api/v1/auth/register', { ...regForm })
    ElMessage.success('注册成功，已自动登录')
    emit('done')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function oauth(provider) {
  location.href = `/api/v1/auth/oauth/${provider}/login`
}

onMounted(async () => {
  const err = new URLSearchParams(location.search).get('login_error')
  if (err) {
    ElMessage.error(err)
    history.replaceState(null, '', '/')
  }
  try {
    const cfg = await api.get('/api/v1/config')
    oauthProviders.value = cfg.oauth_providers || []
    registrationOpen.value = !!cfg.registration_open
  } catch {
    // 配置加载失败时保持基础登录可用
  }
})

onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<style scoped>
.login-wrap {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f5f7fa;
}
.login-card {
  width: 380px;
}
.login-head {
  text-align: center;
  margin-bottom: 8px;
}
.login-head img {
  width: 48px;
  height: 48px;
}
.login-head h2 {
  margin: 8px 0 4px;
}
.login-head .sub {
  color: var(--el-text-color-secondary);
  margin: 0 0 8px;
  font-size: 13px;
}
.submit {
  width: 100%;
}
.code-row {
  display: flex;
  gap: 8px;
  width: 100%;
}
.oauth {
  text-align: center;
}
</style>
