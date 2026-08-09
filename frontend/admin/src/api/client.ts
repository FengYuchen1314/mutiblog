// API 客户端：统一 fetch 封装、CSRF token 获取与错误抛出。
import type { components } from './schema'

type Envelope = components['schemas']

/**
 * 请求后端 JSON API。
 * 返回响应体 data 字段；非 2xx 时抛出带后端错误消息的 Error。
 */
export async function api(path: string, init?: RequestInit) {
  const multipart = init?.body instanceof FormData
  const r = await fetch(path, {
    credentials: 'same-origin',
    headers: {
      ...(multipart ? {} : { 'Content-Type': 'application/json' }),
      ...(init?.headers || {}),
    },
    ...init,
  })
  const data = await r.json().catch(() => null)
  if (!r.ok) throw new Error(data?.error?.message || 'Request failed')
  return data?.data
}

/** 获取 CSRF token（请求会设置同名 cookie，随后用于写操作的请求头）。 */
export async function csrf() {
  const data = await api('/api/auth/csrf')
  return data.token as string
}

export type API = Envelope
