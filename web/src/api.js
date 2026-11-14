// fetch 封装：统一 JSON、错误提示与登录态失效事件
async function request(method, path, body) {
  const opts = { method, headers: {} }
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json'
    opts.body = JSON.stringify(body)
  }
  const resp = await fetch(path, opts)
  if (resp.status === 204) return null
  const data = await resp.json().catch(() => ({}))
  if (!resp.ok) {
    if (resp.status === 401) {
      window.dispatchEvent(new CustomEvent('auth-expired'))
    }
    throw new Error(data.error || `请求失败（${resp.status}）`)
  }
  return data
}

export const api = {
  get: (path) => request('GET', path),
  post: (path, body) => request('POST', path, body),
  put: (path, body) => request('PUT', path, body),
  del: (path) => request('DELETE', path),
}

// 展示与数据分离：后端只回枚举值，前端负责文案
export const levelText = { info: '信息', warning: '警告', error: '错误' }
export const levelTagType = { info: 'info', warning: 'warning', error: 'danger' }
export const roleText = { admin: '平台管理员', tenant_admin: '租户管理员', user: '用户' }
export const scopeText = { all: '全部', own: '本租户', public: '公开' }

export function fmtTime(v) {
  if (!v) return '—'
  return new Date(v).toLocaleString('zh-CN', { hour12: false })
}

export function fmtRelTime(v) {
  if (!v) return '—'
  const diff = Date.now() - new Date(v).getTime()
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)} 小时前`
  return `${Math.floor(diff / 86_400_000)} 天前`
}
