// SSE 订阅：浏览器原生 EventSource（同源自动携带会话 Cookie，断线自动重连）
export function subscribeMessages(onMessage, onState) {
  const es = new EventSource('/api/v1/stream')
  es.addEventListener('message', (e) => {
    try {
      onMessage(JSON.parse(e.data))
    } catch {
      // 忽略无法解析的帧
    }
  })
  es.onopen = () => onState && onState(true)
  es.onerror = () => {
    onState && onState(false)
    if (es.readyState === EventSource.CLOSED) {
      // 会话失效等永久错误：交给全局登录态处理
      window.dispatchEvent(new CustomEvent('auth-expired'))
    }
  }
  return () => es.close()
}
