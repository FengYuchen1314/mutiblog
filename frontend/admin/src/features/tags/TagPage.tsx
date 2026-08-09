// 标签管理：编辑、删除与标签合并。
import { useEffect, useState } from 'react'
import { api, csrf, type API } from '../../api/client'

type Tag = API['Tag']

const LOCALES = ['zh-CN', 'en']

export default function TagPage() {
  const [items, setItems] = useState<Tag[]>([])
  const [editing, setEditing] = useState<Tag | null>(null)
  const [from, setFrom] = useState('')
  const [into, setInto] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/tags/')
      .then((data) => setItems(data.items || []))
      .catch((e) => setError(e.message))
  useEffect(() => {
    refresh()
  }, [])
  const save = async () => {
    if (!editing) return
    try {
      const token = await csrf()
      const method = editing.id ? 'PUT' : 'POST'
      const path = editing.id
        ? '/api/admin/tags/' + encodeURIComponent(editing.id)
        : '/api/admin/tags/'
      await api(path, {
        method,
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(editing),
      })
      setNotice('标签已保存。')
      setEditing(null)
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存标签失败')
    }
  }
  const remove = async (tag: Tag) => {
    if (
      !window.confirm('删除标签 ' + (tag.name?.['zh-CN'] || tag.id) + '？文章中的该标签会被移除。')
    )
      return
    try {
      const token = await csrf()
      await api('/api/admin/tags/' + encodeURIComponent(tag.id), {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
      })
      setNotice('标签已删除。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除标签失败')
    }
  }
  const merge = async () => {
    if (!from || !into) {
      setError('请选择来源与目标标签。')
      return
    }
    const message = '将标签合并到 ' + into + '？来源标签将被删除，文章标签会被替换。'
    if (!window.confirm(message)) return
    try {
      const token = await csrf()
      await api('/api/admin/tags/merge', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ from, into }),
      })
      setNotice('标签已合并，站点将刷新。')
      setFrom('')
      setInto('')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '合并标签失败')
    }
  }
  const setLocalized = (key: 'name' | 'description', locale: string, value: string) => {
    setEditing((current) => {
      if (!current) return current
      const map = { ...(current[key] || {}) }
      map[locale] = value
      return { ...current, [key]: map }
    })
  }
  return (
    <section className="tags">
      <div className="activity-heading">
        <h2>标签</h2>
        <button type="button" onClick={() => setEditing({ id: '', name: {}, description: {} })}>
          新建标签
        </button>
      </div>
      <div className="tag-merge">
        <select value={from} onChange={(e) => setFrom(e.target.value)} aria-label="来源标签">
          <option value="">来源标签…</option>
          {items.map((tag) => (
            <option key={tag.id} value={tag.id}>
              {tag.name?.['zh-CN'] || tag.slug || tag.id}
            </option>
          ))}
        </select>
        <span>→</span>
        <select value={into} onChange={(e) => setInto(e.target.value)} aria-label="目标标签">
          <option value="">目标标签…</option>
          {items.map((tag) => (
            <option key={tag.id} value={tag.id}>
              {tag.name?.['zh-CN'] || tag.slug || tag.id}
            </option>
          ))}
        </select>
        <button type="button" className="secondary" onClick={() => void merge()}>
          合并标签
        </button>
      </div>
      <div className="tag-list">
        {items.map((tag) => (
          <article key={tag.id} className="tag-item">
            <strong>{tag.name?.['zh-CN'] || tag.slug || tag.id}</strong>
            <small>{tag.id}</small>
            <div>
              <button type="button" className="secondary" onClick={() => setEditing({ ...tag })}>
                编辑
              </button>
              <button type="button" className="danger" onClick={() => void remove(tag)}>
                删除
              </button>
            </div>
          </article>
        ))}
        {items.length === 0 && <p>暂无标签。</p>}
      </div>
      {editing && (
        <form
          className="tag-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <h3>{editing.id ? '编辑标签' : '新建标签'}</h3>
          <label>
            ID
            <input
              required
              value={editing.id}
              disabled={Boolean(editing.id)}
              onChange={(e) => setEditing({ ...editing, id: e.target.value })}
            />
          </label>
          <label>
            Slug
            <input
              value={editing.slug || ''}
              onChange={(e) => setEditing({ ...editing, slug: e.target.value })}
            />
          </label>
          {LOCALES.map((locale) => (
            <div className="tag-locale" key={locale}>
              <h4>{locale}</h4>
              <label>
                名称
                <input
                  value={editing.name?.[locale] || ''}
                  onChange={(e) => setLocalized('name', locale, e.target.value)}
                />
              </label>
              <label>
                描述
                <textarea
                  rows={2}
                  value={editing.description?.[locale] || ''}
                  onChange={(e) => setLocalized('description', locale, e.target.value)}
                />
              </label>
            </div>
          ))}
          <div className="theme-actions">
            <button>保存</button>
            <button type="button" className="secondary" onClick={() => setEditing(null)}>
              取消
            </button>
          </div>
        </form>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
