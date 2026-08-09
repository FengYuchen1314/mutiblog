// 日志页：系统日志与审计记录双 Tab，支持级别/组件/操作者筛选。
import { useEffect, useState } from 'react'
import { api, type API } from '../../api/client'

type LogEntry = API['LogEntry']

export default function LogPage() {
  const [tab, setTab] = useState<'logs' | 'audit'>('logs')
  const [items, setItems] = useState<LogEntry[]>([])
  const [level, setLevel] = useState('')
  const [component, setComponent] = useState('')
  const [actor, setActor] = useState('')
  const [action, setAction] = useState('')
  const [error, setError] = useState('')
  const refresh = () => {
    const path =
      tab === 'logs'
        ? '/api/admin/system/logs?limit=200&level=' +
          encodeURIComponent(level) +
          '&component=' +
          encodeURIComponent(component)
        : '/api/admin/system/audit?limit=200&actor=' +
          encodeURIComponent(actor) +
          '&action=' +
          encodeURIComponent(action)
    api(path)
      .then((data) => setItems(data.items || []))
      .catch((e) => setError(e instanceof Error ? e.message : '读取日志失败'))
  }
  useEffect(() => {
    refresh()
  }, [tab])
  return (
    <section className="logs">
      <h2>日志</h2>
      <div className="log-tabs">
        <button
          type="button"
          className={tab === 'logs' ? 'selected' : 'secondary'}
          onClick={() => setTab('logs')}
        >
          系统日志
        </button>
        <button
          type="button"
          className={tab === 'audit' ? 'selected' : 'secondary'}
          onClick={() => setTab('audit')}
        >
          审计记录
        </button>
        <button type="button" className="secondary" onClick={refresh}>
          刷新
        </button>
      </div>
      <form
        className="log-filters"
        onSubmit={(e) => {
          e.preventDefault()
          refresh()
        }}
      >
        {tab === 'logs' ? (
          <>
            <select value={level} onChange={(e) => setLevel(e.target.value)} aria-label="级别">
              <option value="">全部级别</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </select>
            <input
              value={component}
              onChange={(e) => setComponent(e.target.value)}
              placeholder="组件（如 render）"
            />
          </>
        ) : (
          <>
            <input value={actor} onChange={(e) => setActor(e.target.value)} placeholder="操作者" />
            <input
              value={action}
              onChange={(e) => setAction(e.target.value)}
              placeholder="动作（如 auth.login）"
            />
          </>
        )}
        <button>筛选</button>
      </form>
      <div className="log-list">
        {items.map((item) => (
          <article key={item.id} className="log-item">
            <div>
              <strong>{item.level || 'info'}</strong>
              {tab === 'logs' ? (
                <span>
                  {item.component} · {item.message}
                </span>
              ) : (
                <span>
                  {item.actor} · {item.action}
                </span>
              )}
            </div>
            <small>{item.createdAt}</small>
          </article>
        ))}
        {items.length === 0 && <p>暂无日志。</p>}
      </div>
      {error && <p className="error">{error}</p>}
    </section>
  )
}
