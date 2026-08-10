// 实时活动状态：SSE 推送渲染/翻译进度，断线降级为 5 秒轮询。
import { useEffect, useState } from 'react'
import { api } from '../api/client'

export type ActivitySnapshot = {
  jobs: {
    pending: number
    running: number
    failed: number
    oldestRenderStart: string | null
  }
  translations: Array<{
    id: number
    articleID: string
    targetLocale: string
    status: string
    segmentsTotal: number
    segmentsDone: number
    startedAt: string
    error: string
  }>
}

const EMPTY: ActivitySnapshot = {
  jobs: { pending: 0, running: 0, failed: 0, oldestRenderStart: null },
  translations: [],
}

export function useActivity() {
  const [activity, setActivity] = useState<ActivitySnapshot>(EMPTY)
  const [degraded, setDegraded] = useState(false)
  useEffect(() => {
    let eventSource: EventSource | null = null
    let pollTimer: number | null = null
    let closed = false
    const poll = () => {
      api('/api/admin/system/activity')
        .then((data) => {
          if (!closed) setActivity(data)
        })
        .catch(() => {})
    }
    try {
      eventSource = new EventSource('/api/admin/system/events')
      eventSource.onmessage = (event) => {
        if (closed) return
        try {
          setActivity(JSON.parse(event.data))
          setDegraded(false)
        } catch {
          // 忽略坏帧
        }
      }
      eventSource.onerror = () => {
        if (closed) return
        setDegraded(true)
        if (pollTimer == null) {
          poll()
          pollTimer = window.setInterval(poll, 5000)
        }
      }
    } catch {
      setDegraded(true)
      poll()
      pollTimer = window.setInterval(poll, 5000)
    }
    return () => {
      closed = true
      eventSource?.close()
      if (pollTimer != null) window.clearInterval(pollTimer)
    }
  }, [])
  return { activity, degraded }
}
