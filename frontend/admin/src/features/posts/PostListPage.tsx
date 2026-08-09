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
  const refresh = () =>
    api('/api/admin/posts/?locale=zh-CN')
      .then((d) => setPosts(d.items || []))
      .catch((e) => setError(e.message))
  useEffect(refresh, [])
  const create = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    try {
      const token = await csrf()
      await api('/api/admin/posts/', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ title: f.get('title'), body: f.get('body'), locale: 'zh-CN' }),
      })
      e.currentTarget.reset()
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
  return (
    <section>
      <h2>文章</h2>
      <form className="composer" onSubmit={create}>
        <input required name="title" placeholder="文章标题" />
        <textarea required name="body" placeholder="Markdown 正文" />
        <button>新建草稿</button>
      </form>
      {error && <p className="error">{error}</p>}
      <div className="postlist">
        {posts.map((post) => (
          <article key={post.id}>
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
        {posts.length === 0 && <p>还没有文章。创建第一篇草稿吧。</p>}
      </div>
    </section>
  )
}
