<template>
  <div>
    <div v-if="isAdmin" class="stats">
      <el-card shadow="never"><b>{{ stats.tenants }}</b><span>租户</span></el-card>
      <el-card shadow="never"><b>{{ stats.users }}</b><span>用户</span></el-card>
      <el-card shadow="never"><b>{{ stats.tokens }}</b><span>有效令牌</span></el-card>
      <el-card shadow="never"><b>{{ stats.messages }}</b><span>消息</span></el-card>
    </div>

    <el-tabs v-model="tab" @tab-change="loadCurrent">
      <el-tab-pane v-if="isAdmin" label="租户管理" name="tenants" />
      <el-tab-pane label="用户管理" name="users" />
    </el-tabs>

    <template v-if="tab === 'tenants' && isAdmin">
      <div class="toolbar">
        <el-button type="primary" @click="openTenant()">新建租户</el-button>
      </div>
      <el-table :data="tenants" v-loading="loading" empty-text="暂无租户">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column prop="user_count" label="用户数" width="100" />
        <el-table-column label="创建时间" width="180">
          <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="160">
          <template #default="{ row }">
            <el-button text type="primary" @click="openTenant(row)">重命名</el-button>
            <el-button text type="danger" :disabled="row.name === 'default'" @click="removeTenant(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <template v-else>
      <div class="toolbar">
        <el-select v-if="isAdmin" v-model="tenantFilter" placeholder="全部租户" clearable style="width: 180px" @change="loadUsers">
          <el-option v-for="t in tenants" :key="t.id" :label="t.name" :value="t.id" />
        </el-select>
        <div class="spacer" />
        <el-button type="primary" @click="openUser()">新建用户</el-button>
      </div>
      <el-table :data="users" v-loading="loading" empty-text="暂无用户">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="email" label="邮箱" min-width="200" />
        <el-table-column prop="display_name" label="昵称" width="140" />
        <el-table-column label="角色" width="120">
          <template #default="{ row }">
            <el-tag :type="row.role === 'admin' ? 'danger' : row.role === 'tenant_admin' ? 'warning' : 'info'" size="small">
              {{ roleText[row.role] || row.role }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="租户" width="130">
          <template #default="{ row }">{{ row.tenant?.name || '—' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="130">
          <template #default="{ row }">
            <el-button text type="primary" @click="openUser(row)">编辑</el-button>
            <el-button text type="danger" @click="removeUser(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </template>

    <el-dialog v-model="tenantVisible" :title="tenantForm.id ? '重命名租户' : '新建租户'" width="400px">
      <el-form label-width="70px">
        <el-form-item label="名称">
          <el-input v-model="tenantForm.name" maxlength="64" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="tenantVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveTenant">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="userVisible" :title="userForm.id ? '编辑用户' : '新建用户'" width="460px">
      <el-form label-width="90px">
        <el-form-item label="邮箱">
          <el-input v-model="userForm.email" :disabled="!!userForm.id" />
        </el-form-item>
        <el-form-item label="昵称">
          <el-input v-model="userForm.display_name" maxlength="64" />
        </el-form-item>
        <el-form-item label="角色">
          <el-select v-model="userForm.role" :disabled="!isAdmin" style="width: 100%">
            <el-option label="平台管理员" value="admin" :disabled="!isAdmin" />
            <el-option label="租户管理员" value="tenant_admin" />
            <el-option label="用户" value="user" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="isAdmin" label="租户">
          <el-select v-model="userForm.tenant_id" style="width: 100%">
            <el-option v-for="t in tenants" :key="t.id" :label="t.name" :value="t.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="userForm.id ? '重置密码' : '密码'">
          <el-input v-model="userForm.password" type="password" show-password :placeholder="userForm.id ? '留空则不修改' : '至少 8 位'" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveUser">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmtTime, roleText } from '../api'

const props = defineProps({ user: { type: Object, required: true } })
const isAdmin = computed(() => props.user?.role === 'admin')

const tab = ref('tenants')
const loading = ref(false)
const saving = ref(false)
const stats = ref({ tenants: 0, users: 0, tokens: 0, messages: 0 })
const tenants = ref([])
const users = ref([])
const tenantFilter = ref(null)
const tenantVisible = ref(false)
const userVisible = ref(false)
const tenantForm = reactive({ id: 0, name: '' })
const userForm = reactive({ id: 0, email: '', display_name: '', role: 'user', tenant_id: null, password: '' })

function loadCurrent() {
  if (tab.value === 'tenants') loadTenants()
  else loadUsers()
}

async function loadStats() {
  if (!isAdmin.value) return
  try {
    stats.value = await api.get('/api/v1/stats')
  } catch {
    // 统计失败不影响管理功能
  }
}

async function loadTenants() {
  loading.value = true
  try {
    tenants.value = await api.get('/api/v1/tenants')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

async function loadUsers() {
  loading.value = true
  try {
    let path = '/api/v1/users'
    if (isAdmin.value && tenantFilter.value) path += `?tenant_id=${tenantFilter.value}`
    users.value = await api.get(path)
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function openTenant(row) {
  tenantForm.id = row?.id || 0
  tenantForm.name = row?.name || ''
  tenantVisible.value = true
}

async function saveTenant() {
  if (!tenantForm.name.trim()) {
    ElMessage.warning('请输入租户名')
    return
  }
  saving.value = true
  try {
    if (tenantForm.id) {
      await api.put(`/api/v1/tenants/${tenantForm.id}`, { name: tenantForm.name })
    } else {
      await api.post('/api/v1/tenants', { name: tenantForm.name })
    }
    ElMessage.success('已保存')
    tenantVisible.value = false
    loadTenants()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}

async function removeTenant(row) {
  try {
    await ElMessageBox.confirm(`确定删除租户「${row.name}」？租户下存在用户时无法删除。`, '删除租户', { type: 'warning' })
  } catch {
    return
  }
  try {
    await api.del(`/api/v1/tenants/${row.id}`)
    ElMessage.success('已删除')
    loadTenants()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

function openUser(row) {
  userForm.id = row?.id || 0
  userForm.email = row?.email || ''
  userForm.display_name = row?.display_name || ''
  userForm.role = row?.role || 'user'
  userForm.tenant_id = row?.tenant?.id || null
  userForm.password = ''
  userVisible.value = true
}

async function saveUser() {
  if (!userForm.id && (!userForm.email || !userForm.password)) {
    ElMessage.warning('请填写邮箱和初始密码')
    return
  }
  saving.value = true
  try {
    if (userForm.id) {
      const body = { display_name: userForm.display_name, role: userForm.role }
      if (isAdmin.value && userForm.tenant_id) body.tenant_id = userForm.tenant_id
      if (userForm.password) body.password = userForm.password
      await api.put(`/api/v1/users/${userForm.id}`, body)
    } else {
      const body = {
        email: userForm.email,
        password: userForm.password,
        display_name: userForm.display_name,
        role: userForm.role,
      }
      if (isAdmin.value && userForm.tenant_id) body.tenant_id = userForm.tenant_id
      await api.post('/api/v1/users', body)
    }
    ElMessage.success('已保存')
    userVisible.value = false
    loadUsers()
    loadStats()
    loadTenants()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    saving.value = false
  }
}

async function removeUser(row) {
  try {
    await ElMessageBox.confirm(`确定删除用户「${row.email}」？该操作不可恢复。`, '删除用户', { type: 'warning' })
  } catch {
    return
  }
  try {
    await api.del(`/api/v1/users/${row.id}`)
    ElMessage.success('已删除')
    loadUsers()
    loadStats()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

onMounted(() => {
  if (isAdmin.value) {
    tab.value = 'tenants'
    loadTenants()
    loadStats()
  } else {
    tab.value = 'users'
    loadUsers()
  }
})
</script>

<style scoped>
.stats {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
}
.stats .el-card {
  flex: 1;
  text-align: center;
}
.stats b {
  display: block;
  font-size: 24px;
}
.stats span {
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.spacer {
  flex: 1;
}
</style>
