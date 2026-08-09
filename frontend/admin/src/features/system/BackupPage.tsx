// 备份管理：创建、下载、恢复与删除备份。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'
import type { BackupItem } from '../../api/types'

export default function BackupPage() {
  const [items, setItems] = useState<BackupItem[]>([]),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/backups/')
      .then((data) => {
        setItems(data.items || [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取备份失败'))
  useEffect(() => {
    refresh()
  }, [])
  const create = async () => {
    try {
      const token = await csrf()
      await api('/api/admin/backups/', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: '{}',
      })
      setNotice('备份已创建。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建备份失败')
    }
  }
  const remove = async (name: string) => {
    if (!window.confirm('删除此备份？此操作不可撤销。')) return
    try {
      const token = await csrf()
      await api('/api/admin/backups/' + encodeURIComponent(name), {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
      })
      setNotice('备份已删除。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除备份失败')
    }
  }
  const restore = async (name: string) => {
    if (!window.confirm('恢复会替换当前内容、数据、媒体和配置。确定继续吗？')) return
    try {
      const token = await csrf()
      await api('/api/admin/backups/restore', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ name, confirm: true }),
      })
      setNotice('备份已恢复。请重启服务以加载恢复后的配置。')
    } catch (e) {
      setError(e instanceof Error ? e.message : '恢复备份失败')
    }
  }
  return (
    <section className="backups">
      <div className="activity-heading">
        <h2>备份</h2>
        <button type="button" onClick={() => void create()}>
          创建备份
        </button>
      </div>
      {items.length ? (
        <div className="backup-list">
          {items.map((item) => {
            const name = item.path.split('/').pop() || item.path
            return (
              <article key={item.path}>
                <div>
                  <strong>{name}</strong>
                  <small>
                    {Math.ceil(item.size / 1024)} KB · {item.createdAt}
                  </small>
                </div>
                <div>
                  <a
                    className="download"
                    href={'/api/admin/backups/' + encodeURIComponent(name) + '/download'}
                  >
                    下载
                  </a>
                  <button type="button" className="secondary" onClick={() => void restore(name)}>
                    恢复
                  </button>
                  <button type="button" className="danger" onClick={() => void remove(name)}>
                    删除
                  </button>
                </div>
              </article>
            )
          })}
        </div>
      ) : (
        <p>暂无备份。</p>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
