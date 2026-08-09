// 独立页面管理：列表、新建、编辑（模板/排序/显示在菜单）与发布。
import { useEffect, useState } from 'react'
import { api, csrf, type API } from '../../api/client'
import MarkdownEditor from '../../components/MarkdownEditor'

type PageSummary = {
  id: string
  title: string
  slug: string
  status: string
}

type PageDetail = API['PostDetail'] & {
  front: {
    template?: string
    order?: number
    showInMenu?: boolean
  }
}

export default function PagesPage() {
  const [pages, setPages] = useState<PageSummary[]>([])
  const [editing, setEditing] = useState<PageDetail | null>(null)
  const [title, setTitle] = useState('')
  const [slug, setSlug] = useState('')
  const [body, setBody] = useState('')
  const [description, setDescription] = useState('')
  const [template, setTemplate] = useState('')
  const [order, setOrder] = useState(0)
  const [showInMenu, setShowInMenu] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/pages/?locale=zh-CN')
      .then((data) => setPages(data.items || []))
      .catch((e) => setError(e.message))
  useEffect(() => {
    refresh()
  }, [])
  const open = async (id: string) => {
    try {
      const value = await api('/api/admin/pages/' + id + '?locale=zh-CN')
      setEditing(value)
      setTitle(value.front.title)
      setSlug(value.front.slug)
      setDescription(value.front.description || '')
      setBody(value.body)
      setTemplate(value.front.template || '')
      setOrder(value.front.order || 0)
      setShowInMenu(Boolean(value.front.showInMenu))
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取页面失败')
    }
  }
  const create = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    try {
      const token = await csrf()
      await api('/api/admin/pages/', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ title: form.get('title'), body: '', locale: 'zh-CN' }),
      })
      e.currentTarget.reset()
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建页面失败')
    }
  }
  const save = async (publish: boolean) => {
    if (!editing) return
    try {
      const token = await csrf()
      await api('/api/admin/pages/' + editing.id, {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({
          locale: 'zh-CN',
          baseHash: editing.baseHash || '',
          title,
          slug,
          description,
          body,
          template: template || undefined,
          order,
          showInMenu,
        }),
      })
      if (publish) {
        await api('/api/admin/pages/' + editing.id + '/publish', {
          method: 'POST',
          headers: { 'X-CSRF-Token': token },
          body: '{}',
        })
      }
      setNotice(publish ? '页面已发布。' : '页面已保存。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存页面失败')
    }
  }
  const remove = async (id: string) => {
    if (!window.confirm('删除此页面？')) return
    try {
      const token = await csrf()
      await api('/api/admin/pages/' + id, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
        body: '{}',
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除页面失败')
    }
  }
  const publishPage = async (id: string) => {
    try {
      const token = await csrf()
      await api('/api/admin/pages/' + id + '/publish', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: '{}',
      })
      setNotice('页面已发布。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '发布页面失败')
    }
  }
  return (
    <section className="pages">
      <h2>独立页面</h2>
      <form className="composer" onSubmit={create}>
        <input required name="title" placeholder="页面标题" />
        <button>新建页面</button>
      </form>
      <div className="postlist">
        {pages.map((page) => (
          <article key={page.id}>
            <button className="posttitle" type="button" onClick={() => void open(page.id)}>
              <strong>{page.title}</strong>
              <span>
                /{page.slug} · {page.status}
              </span>
            </button>
            <div className="postactions">
              <button type="button" className="secondary" onClick={() => void open(page.id)}>
                编辑
              </button>
              {page.status === 'draft' && (
                <button type="button" onClick={() => void publishPage(page.id)}>
                  发布
                </button>
              )}
              <button type="button" className="danger" onClick={() => void remove(page.id)}>
                删除
              </button>
            </div>
          </article>
        ))}
        {pages.length === 0 && <p>暂无页面。</p>}
      </div>
      {editing && (
        <section className="editor">
          <div className="editorbar">
            <button type="button" onClick={() => setEditing(null)}>
              ← 返回页面
            </button>
            <button type="button" onClick={() => void save(false)}>
              保存
            </button>
            <button type="button" onClick={() => void save(true)}>
              发布
            </button>
          </div>
          <input
            className="editor-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            aria-label="标题"
          />
          <label>
            固定链接
            <input value={slug} onChange={(e) => setSlug(e.target.value)} />
          </label>
          <label>
            摘要
            <textarea value={description} onChange={(e) => setDescription(e.target.value)} />
          </label>
          <div className="editor-grid">
            <MarkdownEditor value={body} onChange={setBody} />
            <aside>
              <label>
                模板
                <input value={template} onChange={(e) => setTemplate(e.target.value)} />
              </label>
              <label>
                排序
                <input
                  type="number"
                  value={order}
                  onChange={(e) => setOrder(Number(e.target.value))}
                />
              </label>
              <label className="check">
                <input
                  type="checkbox"
                  checked={showInMenu}
                  onChange={(e) => setShowInMenu(e.target.checked)}
                />
                显示在菜单
              </label>
            </aside>
          </div>
        </section>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
