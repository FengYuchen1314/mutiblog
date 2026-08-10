// 文章列表：新建草稿、发布、取消发布、删除与恢复。
import { useEffect, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { api, csrf } from '../../api/client'
import type { Post } from '../../api/types'

export default function PostListPage() {
  const navigate = useNavigate()
  const [posts, setPosts] = useState<Post[]>([])
  const [error, setError] = useState('')
  const [publishAt, setPublishAt] = useState('')
  const [locale, setLocale] = useState('zh-CN')
  const [statusFilter, setStatusFilter] = useState<'all' | 'draft' | 'published' | 'trashed'>('all')
  const [categoryFilter, setCategoryFilter] = useState('')
  const [tagFilter, setTagFilter] = useState('')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const refresh = () =>
    api('/api/admin/posts/?locale=' + encodeURIComponent(locale) + '&perPage=200')
      .then((d) => setPosts(d.items || []))
      .catch((e) => setError(e.message))
  useEffect(refresh, [locale])
  const create = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = e.currentTarget
    const f = new FormData(form)
    try {
      const token = await csrf()
      await api('/api/admin/posts/', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ title: f.get('title'), body: f.get('body'), locale: 'zh-CN' }),
      })
      form.reset()
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建失败')
    }
  }
  const publish = async (id: string, scheduled = false) => {
    try {
      const token = await csrf()
      if (scheduled && (!publishAt || Number.isNaN(Date.parse(publishAt))))
        throw new Error('请选择未来发布时间')
      const body = scheduled ? { publishAt: new Date(publishAt).toISOString() } : {}
      await api('/api/admin/posts/' + id + '/publish', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(body),
      })
      setPublishAt('')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '发布失败')
    }
  }
  const act = async (id: string, method: string, path: string) => {
    try {
      const token = await csrf()
      await api('/api/admin/posts/' + id + path, {
        method,
        headers: { 'X-CSRF-Token': token },
        body: '{}',
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '操作失败')
    }
  }
  const categories = [...new Set(posts.flatMap((post) => post.categories || []))]
  const tags = [...new Set(posts.flatMap((post) => post.tags || []))]
  const filtered = posts.filter((post) => {
    if (statusFilter !== 'all' && post.status !== statusFilter) return false
    if (categoryFilter && !(post.categories || []).includes(categoryFilter)) return false
    if (tagFilter && !(post.tags || []).includes(tagFilter)) return false
    return true
  })
  const toggleSelect = (id: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const toggleAll = () => {
    setSelected((current) => {
      const ids = filtered.map((post) => post.id)
      const allSelected = ids.every((id) => current.has(id))
      const next = new Set(current)
      for (const id of ids) {
        if (allSelected) next.delete(id)
        else next.add(id)
      }
      return next
    })
  }
  const batch = async (operation: 'publish' | 'trash') => {
    if (selected.size === 0) return
    const message =
      operation === 'publish'
        ? '批量发布 ' + selected.size + ' 篇文章？'
        : '将 ' + selected.size + ' 篇文章移到回收站？'
    if (!window.confirm(message)) return
    for (const id of selected) {
      if (operation === 'publish') await publish(id)
      else await act(id, 'DELETE', '')
    }
    setSelected(new Set())
  }
  return (
    <section>
      <h2>文章</h2>
      <form className="composer" onSubmit={create}>
        <input required name="title" placeholder="文章标题" />
        <textarea required name="body" placeholder="Markdown 正文" />
        <button>新建草稿</button>
      </form>
      {error && <p className="error">{error}</p>}
      <div className="post-toolbar">
        <select value={locale} onChange={(e) => setLocale(e.target.value)} aria-label="语言">
          <option value="zh-CN">中文</option>
          <option value="en">English</option>
          <option value="zh-TW">繁體中文</option>
          <option value="ja">日本語</option>
          <option value="de">Deutsch</option>
        </select>
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as typeof statusFilter)}
          aria-label="状态"
        >
          <option value="all">全部状态</option>
          <option value="draft">草稿</option>
          <option value="published">已发布</option>
          <option value="trashed">回收站</option>
        </select>
        <select
          value={categoryFilter}
          onChange={(e) => setCategoryFilter(e.target.value)}
          aria-label="分类"
        >
          <option value="">全部分类</option>
          {categories.map((category) => (
            <option key={category} value={category}>
              {category}
            </option>
          ))}
        </select>
        <select value={tagFilter} onChange={(e) => setTagFilter(e.target.value)} aria-label="标签">
          <option value="">全部标签</option>
          {tags.map((tag) => (
            <option key={tag} value={tag}>
              {tag}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="secondary"
          disabled={selected.size === 0}
          onClick={() => void batch('publish')}
        >
          批量发布
        </button>
        <button
          type="button"
          className="danger"
          disabled={selected.size === 0}
          onClick={() => void batch('trash')}
        >
          批量删除
        </button>
        <label className="check">
          <input
            type="checkbox"
            checked={filtered.length > 0 && filtered.every((post) => selected.has(post.id))}
            onChange={toggleAll}
          />
          全选（{selected.size}）
        </label>
      </div>
      <div className="postlist">
        {filtered.map((post) => (
          <article key={post.id}>
            <input
              type="checkbox"
              checked={selected.has(post.id)}
              onChange={() => toggleSelect(post.id)}
              aria-label="选择文章"
            />
            <button
              className="posttitle"
              type="button"
              onClick={() => navigate({ to: '/posts/$id', params: { id: post.id } })}
            >
              <strong>{post.title}</strong>
              <span>
                /{post.slug} · {post.status}
                <small>{post.id}</small>
              </span>
            </button>
            <div className="postactions">
              {post.status === 'published' && (
                <>
                  <a
                    className="view"
                    href={'/zh-cn/posts/' + encodeURIComponent(post.slug) + '/?__v=' + Date.now()}
                    target="_blank"
                    rel="noreferrer"
                  >
                    查看
                  </a>
                  <button type="button" onClick={() => act(post.id, 'POST', '/unpublish')}>
                    取消发布
                  </button>
                </>
              )}
              {post.status === 'draft' && (
                <>
                  <button type="button" onClick={() => publish(post.id)}>
                    立即发布
                  </button>
                  <input
                    aria-label="计划发布时间"
                    type="datetime-local"
                    value={publishAt}
                    onChange={(e) => setPublishAt(e.target.value)}
                  />
                  <button type="button" onClick={() => publish(post.id, true)}>
                    计划发布
                  </button>
                </>
              )}
              {post.status === 'trashed' && (
                <button type="button" onClick={() => act(post.id, 'POST', '/restore')}>
                  恢复
                </button>
              )}
              <button
                type="button"
                className="danger"
                onClick={() => {
                  if (window.confirm('将文章移到回收站？')) act(post.id, 'DELETE', '')
                }}
              >
                删除
              </button>
            </div>
          </article>
        ))}
        {filtered.length === 0 && <p>没有符合条件的文章。</p>}
      </div>
    </section>
  )
}
