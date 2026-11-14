<template>
  <div>
    <div class="toolbar">
      <el-radio-group v-model="scope" @change="reload">
        <el-radio-button value="all">全部</el-radio-button>
        <el-radio-button value="own">本租户</el-radio-button>
        <el-radio-button value="public">公开</el-radio-button>
      </el-radio-group>
      <div class="spacer" />
      <el-tag :type="live ? 'success' : 'info'" effect="dark" size="small">
        {{ live ? '实时连接中' : '连接断开，自动重连中' }}
      </el-tag>
      <el-button :loading="loading" @click="reload">刷新</el-button>
    </div>

    <el-table :data="items" v-loading="loading" @row-click="open" class="msg-table" empty-text="暂无消息">
      <el-table-column label="级别" width="90">
        <template #default="{ row }">
          <el-tag :type="levelTagType[row.level] || 'info'">{{ levelText[row.level] || row.level }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="title" label="标题" min-width="200" show-overflow-tooltip />
      <el-table-column prop="topic" label="主题" width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ row.topic || '—' }}</template>
      </el-table-column>
      <el-table-column label="可见性" width="90">
        <template #default="{ row }">
          <el-tag v-if="row.public" type="success" size="small">公开</el-tag>
          <el-tag v-else type="info" size="small">租户</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.created_at) }}</template>
      </el-table-column>
    </el-table>

    <div class="more">
      <el-button v-if="nextCursor" :loading="loading" @click="loadMore">加载更多</el-button>
    </div>

    <el-dialog v-model="detail" :title="detail?.title" width="560px">
      <p class="detail-meta">
        <el-tag :type="levelTagType[detail?.level] || 'info'">{{ levelText[detail?.level] || detail?.level }}</el-tag>
        <span v-if="detail?.topic">主题：{{ detail.topic }}</span>
        <span>{{ fmtTime(detail?.created_at) }}</span>
      </p>
      <pre class="detail-body">{{ detail?.body || '（无正文）' }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, onUnmounted, ref } from 'vue'
import { ElNotification } from 'element-plus'
import { api, fmtTime, levelText, levelTagType } from '../api'
import { subscribeMessages } from '../events'

const items = ref([])
const scope = ref('all')
const nextCursor = ref(null)
const loading = ref(false)
const live = ref(false)
const detail = ref(null)
let unsubscribe = null

async function reload() {
  nextCursor.value = null
  items.value = []
  await load()
}

async function load() {
  loading.value = true
  try {
    const query = new URLSearchParams({ limit: '50', scope: scope.value })
    if (nextCursor.value) query.set('cursor', nextCursor.value)
    const data = await api.get(`/api/v1/messages?${query}`)
    items.value = items.value.concat(data.items || [])
    nextCursor.value = data.next_cursor
  } catch (e) {
    ElNotification.error({ title: '消息加载失败', message: e.message })
  } finally {
    loading.value = false
  }
}

function loadMore() {
  load()
}

function open(row) {
  detail.value = row
}

onMounted(() => {
  load()
  unsubscribe = subscribeMessages(
    (msg) => {
      if (items.value.some((it) => it.id === msg.id)) return
      if (scope.value === 'public' && !msg.public) return
      if (scope.value === 'own' && msg.public) return
      items.value.unshift(msg)
      ElNotification({
        title: msg.title,
        message: msg.body || msg.title,
        type: msg.level === 'error' ? 'error' : msg.level === 'warning' ? 'warning' : 'info',
      })
    },
    (ok) => {
      live.value = ok
    },
  )
})

onUnmounted(() => {
  if (unsubscribe) unsubscribe()
})
</script>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 12px;
}
.spacer {
  flex: 1;
}
.more {
  text-align: center;
  margin-top: 12px;
}
.msg-table {
  cursor: pointer;
}
.detail-meta {
  display: flex;
  gap: 12px;
  align-items: center;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}
.detail-body {
  white-space: pre-wrap;
  word-break: break-all;
  background: #f5f7fa;
  padding: 12px;
  border-radius: 4px;
  font-family: inherit;
}
</style>
