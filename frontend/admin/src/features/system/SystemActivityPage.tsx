// 系统活动：最近系统日志与审计记录。
import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { LogEntry } from '../../api/types'

export default function SystemActivityPage() {
  const [logs, setLogs] = useState<LogEntry[]>([]),
    [audit, setAudit] = useState<LogEntry[]>([]),
    [error, setError] = useState('')
  const refresh = () =>
    Promise.all([api('/api/admin/system/logs?limit=8'), api('/api/admin/system/audit?limit=8')])
      .then(([logData, auditData]) => {
        setLogs(logData.items || [])
        setAudit(auditData.items || [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取系统活动失败'))
  useEffect(() => {
    refresh()
  }, [])
  return (
    <section className="activity">
      <div className="activity-heading">
        <h2>系统活动</h2>
        <button type="button" onClick={refresh}>
          刷新
        </button>
      </div>
      <div className="activity-grid">
        <div>
          <h3>系统日志</h3>
          {logs.length ? (
            logs.map((item) => (
              <p key={item.id}>
                <strong>{item.level || 'info'}</strong> {item.component} · {item.message}
                <small>{item.createdAt}</small>
              </p>
            ))
          ) : (
            <p>暂无系统日志。</p>
          )}
        </div>
        <div>
          <h3>审计记录</h3>
          {audit.length ? (
            audit.map((item) => (
              <p key={item.id}>
                <strong>{item.actor}</strong> · {item.action}
                <small>{item.createdAt}</small>
              </p>
            ))
          ) : (
            <p>暂无审计记录。</p>
          )}
        </div>
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  )
}
