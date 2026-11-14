<template>
  <div>
    <div class="toolbar">
      <el-alert type="info" :closable="false" show-icon
        title="令牌用于服务端推送消息（Bearer 认证）；明文只在创建时显示一次，请立即保存。" />
      <div class="spacer" />
      <el-button type="primary" @click="openCreate">新建令牌</el-button>
    </div>

    <el-table :data="tokens" v-loading="loading" empty-text="还没有令牌">
      <el-table-column prop="name" label="名称" min-width="140" />
      <el-table-column label="限流（次/分钟）" width="140">
        <template #default="{ row }">{{ row.rate_limit_per_min }}</template>
      </el-table-column>
      <el-table-column label="最后使用" width="170">
        <template #default="{ row }">{{ fmtTime(row.last_used_at) }}</template>
      </el-table-column>
      <el-table-column label="创建时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button type="danger" text @click="revoke(row)">吊销</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="createVisible" title="新建令牌" width="420px">
      <el-form label-width="110px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="例如：CI 告警" maxlength="64" />
        </el-form-item>
        <el-form-item label="每分钟上限">
          <el-input-number v-model="form.rate_limit_per_min" :min="1" :max="100000" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="create">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="resultVisible" title="令牌已创建" width="520px" :close-on-click-modal="false">
      <el-alert type="warning" :closable="false" show-icon title="以下明文仅显示这一次，关闭后无法找回。" />
      <div class="token-row">
        <el-input :model-value="createdToken" readonly class="token-input" />
        <el-button type="primary" @click="copy">复制</el-button>
      </div>
      <p class="usage">推送示例：<code>curl -u : -H "Authorization: Bearer {{ createdToken.slice(0, 12) }}…" -d '{"title":"..."}' /api/v1/messages</code></p>
      <template #footer>
        <el-button type="primary" @click="resultVisible = false">我已保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, fmtTime } from '../api'

const tokens = ref([])
const loading = ref(false)
const createVisible = ref(false)
const resultVisible = ref(false)
const creating = ref(false)
const createdToken = ref('')
const form = reactive({ name: '', rate_limit_per_min: 60 })

async function load() {
  loading.value = true
  try {
    tokens.value = await api.get('/api/v1/tokens')
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.name = ''
  form.rate_limit_per_min = 60
  createVisible.value = true
}

async function create() {
  creating.value = true
  try {
    const dto = await api.post('/api/v1/tokens', { ...form })
    createdToken.value = dto.token
    createVisible.value = false
    resultVisible.value = true
    load()
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    creating.value = false
  }
}

async function revoke(row) {
  try {
    await ElMessageBox.confirm(`确定吊销令牌「${row.name}」？使用它的推送将立即失效。`, '吊销令牌', { type: 'warning' })
  } catch {
    return
  }
  try {
    await api.del(`/api/v1/tokens/${row.id}`)
    ElMessage.success('已吊销')
    load()
  } catch (e) {
    ElMessage.error(e.message)
  }
}

async function copy() {
  try {
    await navigator.clipboard.writeText(createdToken.value)
    ElMessage.success('已复制到剪贴板')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

onMounted(load)
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.toolbar .el-alert {
  flex: 1;
}
.spacer {
  width: 10px;
}
.token-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
.token-input :deep(input) {
  font-family: monospace;
}
.usage {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  word-break: break-all;
}
</style>
