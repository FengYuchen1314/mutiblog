// 站点设置：读取并保存站点标题。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'

export default function SiteSettingsPage() {
  const [title, setTitle] = useState(''),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('')
  useEffect(() => {
    api('/api/admin/settings/')
      .then((value) => setTitle(value.values?.site?.title || ''))
      .catch((e) => setError(e.message))
  }, [])
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const token = await csrf()
      const result = await api('/api/admin/settings/site', {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ title }),
      })
      const notice = result.requiresRestart
        ? '设置已保存，重启后生效。'
        : '设置已保存，站点将刷新。'
      setNotice(notice)
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存设置失败')
    }
  }
  return (
    <section className="settings">
      <h2>站点设置</h2>
      <form onSubmit={save}>
        <label>
          站点标题
          <input required value={title} onChange={(e) => setTitle(e.target.value)} />
        </label>
        <button>保存设置</button>
      </form>
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
