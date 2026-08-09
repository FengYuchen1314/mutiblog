// 回收站：已删除文章列表、恢复与永久删除。
import { useEffect, useState } from 'react'
import { api, csrf, type API } from '../../api/client'

type PostSummary = API['PostSummary']

export default function TrashPage() {
  const [posts, setPosts] = useState<PostSummary[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/posts/?locale=zh-CN&perPage=200')
      .then((data) =>
        setPosts((data.items || []).filter((post: PostSummary) => post.status === 'trashed')),
      )
      .catch((e) => setError(e.message))
  useEffect(() => {
    refresh()
  }, [])
  const restore = async (id: string) => {
    try {
      const token = await csrf()
      await api('/api/admin/posts/' + id + '/restore', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: '{}',
      })
      setNotice('文章已恢复。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '恢复文章失败')
    }
  }
  const purge = async (id: string, title: string) => {
    if (!window.confirm('永久删除「' + title + '」？此操作不可恢复。')) return
    try {
      const token = await csrf()
      await api('/api/admin/posts/' + id + '/purge', {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
      })
      setNotice('文章已永久删除。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '永久删除失败')
    }
  }
  return (
    <section className="trash">
      <div className="activity-heading">
        <h2>回收站</h2>
        <button type="button" onClick={refresh}>
          刷新
        </button>
      </div>
      <div className="postlist">
        {posts.map((post) => (
          <article key={post.id}>
            <div>
              <strong>{post.title}</strong>
              <small>/{post.slug}</small>
            </div>
            <div className="postactions">
              <button type="button" className="secondary" onClick={() => void restore(post.id)}>
                恢复
              </button>
              <button
                type="button"
                className="danger"
                onClick={() => void purge(post.id, post.title)}
              >
                永久删除
              </button>
            </div>
          </article>
        ))}
        {posts.length === 0 && <p>回收站是空的。</p>}
      </div>
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
