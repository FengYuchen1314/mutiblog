import { useEffect, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { markdown } from '@codemirror/lang-markdown'
import { oneDark } from '@codemirror/theme-one-dark'
import type { components } from './api/schema'
import './styles.css'
type API = components['schemas']
async function api(path: string, init?: RequestInit) {
  const multipart = init?.body instanceof FormData
  const r = await fetch(path, {
    credentials: 'same-origin',
    headers: {
      ...(multipart ? {} : { 'Content-Type': 'application/json' }),
      ...(init?.headers || {}),
    },
    ...init,
  })
  const data = await r.json().catch(() => null)
  if (!r.ok) throw new Error(data?.error?.message || 'Request failed')
  return data?.data
}
function Setup({ done }: { done: () => void }) {
  const [error, setError] = useState('')
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    try {
      await api('/api/auth/setup', {
        method: 'POST',
        body: JSON.stringify({
          username: f.get('username'),
          email: f.get('email'),
          password: f.get('password'),
          locale: 'zh-CN',
        }),
      })
      done()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Setup failed')
    }
  }
  return (
    <main className="card">
      <h1>设置 Mutiblog</h1>
      <p>创建第一位管理员以开始管理站点。</p>
      <form onSubmit={submit}>
        <input required name="username" placeholder="用户名" />
        <input required type="email" name="email" placeholder="邮箱" />
        <input
          required
          minLength={12}
          type="password"
          name="password"
          placeholder="密码（至少 12 位）"
        />
        <button>创建管理员</button>
        {error && <p className="error">{error}</p>}
      </form>
    </main>
  )
}
function Login({ done }: { done: () => void }) {
  const [error, setError] = useState('')
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    try {
      await api('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username: f.get('username'), password: f.get('password') }),
      })
      done()
    } catch (e) {
      setError(e instanceof Error ? e.message : '登录失败')
    }
  }
  return (
    <main className="card">
      <h1>登录 Mutiblog</h1>
      <form onSubmit={submit}>
        <input required name="username" placeholder="用户名" />
        <input required type="password" name="password" placeholder="密码" />
        <button>登录</button>
        {error && <p className="error">{error}</p>}
      </form>
    </main>
  )
}
async function csrf() {
  const data = await api('/api/auth/csrf')
  return data.token as string
}
type Post = API['PostSummary']
type EditorPost = {
  id: string
  locale: string
  baseHash: string
  body: string
  front: {
    title: string
    slug: string
    description?: string
    categories?: string[]
    tags?: string[]
    status: string
  }
}
type TranslationTask = API['TranslationTask']
type MediaItem = API['MediaItem']
function Posts({ onEdit }: { onEdit: (id: string) => void }) {
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
            <button className="posttitle" type="button" onClick={() => onEdit(post.id)}>
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
function MediaManager() {
  const [items, setItems] = useState<MediaItem[]>([]),
    [dir, setDir] = useState(''),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/media/?dir=' + encodeURIComponent(dir))
      .then((d) => setItems(d.items || []))
      .catch((e) => setError(e.message))
  useEffect(refresh, [dir])
  const upload = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    form.set('dir', dir)
    try {
      const token = await csrf()
      const result = await api('/api/admin/media/upload', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: form,
      })
      const failed = (result.items || []).filter((x: MediaItem) => x.ok === false)
      setNotice(
        failed.length
          ? failed.map((x: MediaItem) => x.name + ': ' + x.error).join('；')
          : '上传成功',
      )
      e.currentTarget.reset()
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '上传失败')
    }
  }
  const del = async (item: MediaItem) => {
    if (!window.confirm('删除 ' + item.name + '？此操作不可撤销。')) return
    try {
      const token = await csrf()
      await api('/api/admin/media/', {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ paths: [item.path] }),
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除失败')
    }
  }
  const mkdir = async () => {
    const name = window.prompt('新建目录名称：')
    if (!name) return
    try {
      const token = await csrf()
      await api('/api/admin/media/mkdir', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ dir: dir ? dir + '/' + name : name }),
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建目录失败')
    }
  }
  return (
    <section className="media">
      <div className="activity-heading">
        <h2>媒体库</h2>
        <button type="button" onClick={mkdir}>
          新建目录
        </button>
      </div>
      <form className="import-form" onSubmit={upload}>
        <input name="file" type="file" multiple />
        <button>上传</button>
      </form>
      <label>
        目录：
        <input value={dir} onChange={(e) => setDir(e.target.value)} placeholder="留空为根目录" />
      </label>
      {items.length ? (
        <div className="media-grid">
          {items.map((item) =>
            item.isDir ? (
              <div key={item.path} className="media-item">
                <button type="button" className="posttitle" onClick={() => setDir(item.path)}>
                  📁 {item.name}
                </button>
                <button type="button" className="danger" onClick={() => del(item)}>
                  删除
                </button>
              </div>
            ) : (
              <div key={item.path} className="media-item">
                <img src={item.url} alt={item.name} loading="lazy" />
                <a href={item.url} target="_blank" rel="noreferrer">
                  {item.name}
                </a>
                <small>{Math.ceil((item.size || 0) / 1024)} KB</small>
                <button type="button" className="danger" onClick={() => del(item)}>
                  删除
                </button>
              </div>
            ),
          )}
        </div>
      ) : (
        <p>暂无媒体文件。</p>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
function MarkdownEditor({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const host = useRef<HTMLDivElement>(null),
    view = useRef<EditorView | null>(null),
    latest = useRef(value)
  useEffect(() => {
    latest.current = value
  }, [value])
  useEffect(() => {
    if (!host.current) return
    view.current = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          history(),
          keymap.of([...defaultKeymap, ...historyKeymap]),
          markdown(),
          oneDark,
          EditorView.lineWrapping,
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChange(update.state.doc.toString())
          }),
        ],
      }),
      parent: host.current,
    })
    return () => {
      view.current?.destroy()
      view.current = null
    }
  }, [])
  useEffect(() => {
    const current = view.current?.state.doc.toString()
    if (view.current && current !== value && value === latest.current)
      view.current.dispatch({ changes: { from: 0, to: current.length, insert: value } })
  }, [value])
  return <div className="editor-body codemirror" ref={host} />
}
function PostEditor({ id, done }: { id: string; done: () => void }) {
  const [post, setPost] = useState<EditorPost | null>(null),
    [error, setError] = useState(''),
    [preview, setPreview] = useState(''),
    [saving, setSaving] = useState(false),
    [dirty, setDirty] = useState(false),
    [revisions, setRevisions] = useState<number[]>([])
  const [title, setTitle] = useState(''),
    [slug, setSlug] = useState(''),
    [description, setDescription] = useState(''),
    [body, setBody] = useState(''),
    [categories, setCategories] = useState(''),
    [tags, setTags] = useState('')
  const loaded = useRef(false)
  const localKey = 'mutiblog:draft:' + id + ':zh-CN'
  const load = () =>
    api('/api/admin/posts/' + id + '?locale=zh-CN')
      .then((value: EditorPost) => {
        const cached = localStorage.getItem(localKey)
        setPost(value)
        setTitle(value.front.title)
        setSlug(value.front.slug)
        setDescription(value.front.description || '')
        setBody(cached || value.body)
        setCategories((value.front.categories || []).join(', '))
        setTags((value.front.tags || []).join(', '))
        setDirty(Boolean(cached))
        loaded.current = true
      })
      .catch((e) => setError(e.message))
  useEffect(() => {
    load()
  }, [id])
  const payload = () => ({
    locale: 'zh-CN',
    baseHash: post?.baseHash || '',
    title,
    slug,
    description,
    body,
    categories: categories
      .split(',')
      .map((v) => v.trim())
      .filter(Boolean),
    tags: tags
      .split(',')
      .map((v) => v.trim())
      .filter(Boolean),
  })
  const save = async (draft: boolean) => {
    if (!post) return false
    setSaving(true)
    try {
      const token = await csrf()
      await api('/api/admin/posts/' + id + (draft ? '/draft' : ''), {
        method: draft ? 'POST' : 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(payload()),
      })
      if (!draft) await load()
      localStorage.removeItem(localKey)
      setDirty(false)
      setError('')
      return true
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存失败')
      return false
    } finally {
      setSaving(false)
    }
  }
  useEffect(() => {
    if (!loaded.current || !dirty) return
    const timer = window.setTimeout(() => void save(true), 2000)
    return () => window.clearTimeout(timer)
  }, [title, slug, description, body, categories, tags, dirty])
  useEffect(() => {
    if (dirty) localStorage.setItem(localKey, body)
  }, [body, dirty, localKey])
  const previewMarkdown = async () => {
    try {
      const token = await csrf()
      const value = await api('/api/admin/preview/markdown', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ body }),
      })
      setPreview(value.html || '')
    } catch (e) {
      setError(e instanceof Error ? e.message : '预览失败')
    }
  }
  const publish = async () => {
    if (!(await save(false))) return
    try {
      const token = await csrf()
      await api('/api/admin/posts/' + id + '/publish', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: '{}',
      })
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : '发布失败')
    }
  }
  const loadRevisions = () =>
    api('/api/admin/posts/' + id + '/revisions?locale=zh-CN')
      .then((value) =>
        setRevisions((value.items || []).map((item: any) => Number(item.revision || item))),
      )
      .catch((e) => setError(e.message))
  if (!post)
    return (
      <section>
        <button type="button" onClick={done}>
          ← 返回文章
        </button>
        <p>加载编辑器…</p>
        {error && <p className="error">{error}</p>}
      </section>
    )
  const change =
    (fn: (value: string) => void) =>
    (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      fn(event.target.value)
      setDirty(true)
    }
  return (
    <section className="editor">
      <div className="editorbar">
        <button type="button" onClick={done}>
          ← 返回文章
        </button>
        <span>{saving ? '正在保存…' : dirty ? '未保存的更改' : '已保存'}</span>
        <button type="button" onClick={previewMarkdown}>
          预览
        </button>
        <button type="button" onClick={() => void save(false)}>
          保存
        </button>
        <button type="button" onClick={() => void publish()}>
          发布
        </button>
      </div>
      <input className="editor-title" value={title} onChange={change(setTitle)} aria-label="标题" />
      <label>
        固定链接
        <input value={slug} onChange={change(setSlug)} />
      </label>
      <label>
        摘要
        <textarea value={description} onChange={change(setDescription)} />
      </label>
      <div className="editor-grid">
        <MarkdownEditor
          value={body}
          onChange={(value) => {
            setBody(value)
            setDirty(true)
          }}
        />
        <aside>
          <label>
            分类（逗号分隔）
            <input value={categories} onChange={change(setCategories)} />
          </label>
          <label>
            标签（逗号分隔）
            <input value={tags} onChange={change(setTags)} />
          </label>
          <button type="button" onClick={loadRevisions}>
            查看修订
          </button>
          {revisions.length > 0 && <p>可用修订：{revisions.join(', ')}</p>}
        </aside>
      </div>
      {preview && <article className="preview" dangerouslySetInnerHTML={{ __html: preview }} />}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
function SiteSettings() {
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
type ThemeField = {
  key: string
  type: string
  label?: Record<string, string>
  help?: Record<string, string>
  default?: unknown
  options?: Array<{ value: string; label?: Record<string, string> }>
  items?: ThemeField[]
  showIf?: { key: string; equals: unknown }
  placeholder?: string
  pattern?: string
  maxLength?: number
  rows?: number
  min?: number
  max?: number
  step?: number
  required?: boolean
  advanced?: boolean
}
type ThemeItem = {
  manifest: { name: string; displayName?: Record<string, string> }
  schema: { fields?: ThemeField[] }
}
function ThemeArrayField({
  field,
  value,
  onChange,
}: {
  field: ThemeField & { items?: ThemeField[] }
  value: unknown
  onChange: (value: Record<string, unknown>[]) => void
}) {
  const rows = Array.isArray(value)
    ? (value.filter((item) => item && typeof item === 'object') as Record<string, unknown>[])
    : []
  const title = (item: ThemeField) => item.label?.['zh-CN'] || item.label?.en || item.key
  const edit = (row: number, key: string, next: unknown) =>
    onChange(rows.map((item, index) => (index === row ? { ...item, [key]: next } : item)))
  return (
    <fieldset className="theme-array">
      <legend>{title(field)}</legend>
      {rows.map((item, row) => (
        <div className="theme-array-item" key={row}>
          {(field.items || []).map((child) =>
            child.type === 'select' ? (
              <label key={child.key}>
                {title(child)}
                <select
                  value={String(item[child.key] ?? child.default ?? '')}
                  onChange={(e) => edit(row, child.key, e.target.value)}
                >
                  {(child.options || []).map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label?.['zh-CN'] || option.label?.en || option.value}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <label key={child.key}>
                {title(child)}
                <input
                  value={String(item[child.key] ?? child.default ?? '')}
                  onChange={(e) => edit(row, child.key, e.target.value)}
                />
              </label>
            ),
          )}
          <button
            type="button"
            className="danger"
            onClick={() => onChange(rows.filter((_, index) => index !== row))}
          >
            删除
          </button>
        </div>
      ))}
      <button
        type="button"
        className="secondary"
        onClick={() =>
          onChange([
            ...rows,
            Object.fromEntries(
              (field.items || []).map((child) => [child.key, child.default ?? '']),
            ),
          ])
        }
      >
        添加一项
      </button>
    </fieldset>
  )
}
function ThemeI18nField({
  field,
  value,
  onChange,
}: {
  field: ThemeField
  value: unknown
  onChange: (value: Record<string, string>) => void
}) {
  const values =
    value && typeof value === 'object' && !Array.isArray(value)
      ? (value as Record<string, string>)
      : {}
  const locales = [...new Set(['zh-CN', 'en', ...Object.keys(values)])]
  const label = (locale: string) =>
    locale === 'zh-CN' ? '简体中文' : locale === 'en' ? 'English' : locale
  const textarea = field.type === 'i18n-textarea'
  return (
    <fieldset className="theme-i18n">
      <legend>{field.label?.['zh-CN'] || field.label?.en || field.key}</legend>
      {locales.map((locale) => (
        <label key={locale}>
          {label(locale)}
          {textarea ? (
            <textarea
              rows={field.rows || 3}
              value={values[locale] || ''}
              onChange={(e) => onChange({ ...values, [locale]: e.target.value })}
            />
          ) : (
            <input
              value={values[locale] || ''}
              onChange={(e) => onChange({ ...values, [locale]: e.target.value })}
            />
          )}
        </label>
      ))}
    </fieldset>
  )
}
function ThemeSettings() {
  const [items, setItems] = useState<ThemeItem[]>([]),
    [active, setActive] = useState(''),
    [selected, setSelected] = useState<ThemeItem | null>(null),
    [values, setValues] = useState<Record<string, unknown>>({}),
    [notice, setNotice] = useState(''),
    [error, setError] = useState('')
  const select = async (item: ThemeItem) => {
    try {
      const detail = await api('/api/admin/themes/' + encodeURIComponent(item.manifest.name))
      setSelected(detail.theme)
      setValues(detail.values || {})
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取主题失败')
    }
  }
  const refresh = () =>
    api('/api/admin/themes/')
      .then((data) => {
        setItems(data.items || [])
        setActive(data.active || '')
        const current = (data.items || []).find(
          (item: ThemeItem) => item.manifest.name === (selected?.manifest.name || data.active),
        )
        if (current) void select(current)
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取主题列表失败'))
  useEffect(() => {
    refresh()
  }, [])
  const activate = async (name: string) => {
    if (!window.confirm('切换主题会重新生成整个静态站点。继续吗？')) return
    try {
      const token = await csrf()
      await api('/api/admin/themes/' + encodeURIComponent(name) + '/activate', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
      })
      setActive(name)
      setNotice('主题已切换，正在重新生成站点。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '切换主题失败')
    }
  }
  const save = async (reset = false) => {
    if (!selected) return
    try {
      const token = await csrf()
      const suffix = reset ? '/settings/reset' : '/settings'
      const result = await api(
        '/api/admin/themes/' + encodeURIComponent(selected.manifest.name) + suffix,
        {
          method: reset ? 'POST' : 'PUT',
          headers: { 'X-CSRF-Token': token },
          body: reset ? undefined : JSON.stringify(values),
        },
      )
      setValues(result.values || {})
      setNotice(reset ? '已恢复主题默认设置。' : '主题设置已保存，正在重新生成站点。')
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存主题设置失败')
    }
  }
  const fields = (selected?.schema?.fields || []).filter(
    (field) =>
      field.type !== 'group-divider' &&
      (!field.showIf || values[field.showIf.key] === field.showIf.equals),
  )
  const label = (field: ThemeField) => field.label?.['zh-CN'] || field.label?.en || field.key
  const help = (field: ThemeField) => field.help?.['zh-CN'] || field.help?.en
  return (
    <section className="themes">
      <h2>主题</h2>
      <div className="theme-picker">
        {items.map((item) => (
          <button
            key={item.manifest.name}
            className={selected?.manifest.name === item.manifest.name ? 'selected' : ''}
            type="button"
            onClick={() => void select(item)}
          >
            {item.manifest.displayName?.['zh-CN'] ||
              item.manifest.displayName?.en ||
              item.manifest.name}
            {active === item.manifest.name && <small>当前</small>}
          </button>
        ))}
      </div>
      {selected && (
        <form
          className="theme-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <div className="theme-heading">
            <h3>{selected.manifest.displayName?.['zh-CN'] || selected.manifest.name}</h3>
            {active !== selected.manifest.name && (
              <button type="button" onClick={() => void activate(selected.manifest.name)}>
                激活主题
              </button>
            )}
          </div>
          {fields.map((field) => {
            const value = values[field.key] ?? field.default ?? ''
            const meta = help(field) && <small>{help(field)}</small>
            if (field.type === 'array')
              return (
                <div key={field.key}>
                  <ThemeArrayField
                    field={field}
                    value={value}
                    onChange={(next) => setValues({ ...values, [field.key]: next })}
                  />
                  {meta}
                </div>
              )
            if (field.type === 'i18n-text' || field.type === 'i18n-textarea')
              return (
                <div key={field.key}>
                  <ThemeI18nField
                    field={field}
                    value={value}
                    onChange={(next) => setValues({ ...values, [field.key]: next })}
                  />
                  {meta}
                </div>
              )
            if (field.type === 'boolean')
              return (
                <label className="check" key={field.key}>
                  <input
                    type="checkbox"
                    checked={Boolean(value)}
                    onChange={(e) => setValues({ ...values, [field.key]: e.target.checked })}
                  />
                  {label(field)}
                  {meta}
                </label>
              )
            if (field.type === 'radio')
              return (
                <fieldset key={field.key}>
                  <legend>{label(field)}</legend>
                  {(field.options || []).map((option) => (
                    <label className="radio" key={option.value}>
                      <input
                        type="radio"
                        name={field.key}
                        checked={value === option.value}
                        onChange={() => setValues({ ...values, [field.key]: option.value })}
                      />
                      {option.label?.['zh-CN'] || option.label?.en || option.value}
                    </label>
                  ))}
                  {meta}
                </fieldset>
              )
            if (field.type === 'select' || field.type === 'multiselect')
              return (
                <label key={field.key}>
                  {label(field)}
                  <select
                    multiple={field.type === 'multiselect'}
                    value={
                      field.type === 'multiselect'
                        ? Array.isArray(value)
                          ? value.map(String)
                          : []
                        : String(value)
                    }
                    onChange={(e) =>
                      setValues({
                        ...values,
                        [field.key]:
                          field.type === 'multiselect'
                            ? Array.from(e.currentTarget.selectedOptions, (option) => option.value)
                            : e.target.value,
                      })
                    }
                  >
                    {(field.options || []).map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label?.['zh-CN'] || option.label?.en || option.value}
                      </option>
                    ))}
                  </select>
                  {meta}
                </label>
              )
            if (field.type === 'textarea' || field.type === 'code')
              return (
                <label key={field.key}>
                  {label(field)}
                  <textarea
                    rows={field.rows || undefined}
                    maxLength={field.maxLength}
                    placeholder={field.placeholder}
                    required={field.required}
                    value={String(value)}
                    onChange={(e) => setValues({ ...values, [field.key]: e.target.value })}
                  />
                  {meta}
                </label>
              )
            const inputType =
              field.type === 'number'
                ? 'number'
                : field.type === 'color'
                  ? 'color'
                  : field.type === 'url'
                    ? 'url'
                    : 'text'
            return (
              <label key={field.key}>
                {label(field)}
                <input
                  type={inputType}
                  min={field.min}
                  max={field.max}
                  step={field.step}
                  maxLength={field.maxLength}
                  pattern={field.pattern}
                  placeholder={field.placeholder}
                  required={field.required}
                  value={String(value)}
                  onChange={(e) =>
                    setValues({
                      ...values,
                      [field.key]:
                        field.type === 'number' ? Number(e.target.value) : e.target.value,
                    })
                  }
                />
                {meta}
              </label>
            )
          })}
          <div className="theme-actions">
            <button>保存主题设置</button>
            <button type="button" className="secondary" onClick={() => void save(true)}>
              恢复默认
            </button>
          </div>
        </form>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
type LogEntry = {
  id: number
  level?: string
  component?: string
  message?: string
  actor?: string
  action?: string
  createdAt: string
}
function SystemActivity() {
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
type BackupItem = { path: string; size: number; createdAt: string }
function BackupManager() {
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
type ImportResult = {
  jobId: number
  status: string
  phase: string
  report?: { found: number; imported: number; skipped: number; failures?: string[] }
  error?: string
}
function ImportManager() {
  const [job, setJob] = useState<ImportResult | null>(null),
    [error, setError] = useState('')
  useEffect(() => {
    if (!job || ['done', 'failed'].includes(job.status)) return
    const timer = window.setInterval(
      () =>
        api('/api/admin/import/' + job.jobId)
          .then(setJob)
          .catch((e) => setError(e.message)),
      1000,
    )
    return () => window.clearInterval(timer)
  }, [job?.jobId, job?.status])
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    const file = form.get('file')
    if (!(file instanceof File) || !file.size) {
      setError('请选择 Markdown 或 ZIP 文件。')
      return
    }
    try {
      const token = await csrf()
      form.set('dryRun', String(form.get('dryRun') === 'on'))
      form.set('createMissingTaxonomy', String(form.get('createMissingTaxonomy') === 'on'))
      const result = await api('/api/admin/import', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: form,
      })
      setJob({ jobId: result.jobId, status: 'pending', phase: result.phase })
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '导入提交失败')
    }
  }
  return (
    <section className="imports">
      <h2>导入内容</h2>
      <form className="import-form" onSubmit={submit}>
        <input required name="file" type="file" accept=".md,.zip,text/markdown,application/zip" />
        <select name="defaultStatus" defaultValue="draft">
          <option value="draft">导入为草稿</option>
          <option value="published">直接发布</option>
        </select>
        <label className="check">
          <input name="dryRun" type="checkbox" />
          仅预览，不写入
        </label>
        <label className="check">
          <input name="createMissingTaxonomy" type="checkbox" defaultChecked />
          自动创建分类和标签
        </label>
        <button>开始导入</button>
      </form>
      {job && (
        <div className="import-result">
          <strong>
            {job.status === 'failed' ? '导入失败' : job.status === 'done' ? '导入完成' : '正在导入'}{' '}
            · {job.phase}
          </strong>
          {job.report && (
            <p>
              发现 {job.report.found}，已导入 {job.report.imported}，跳过 {job.report.skipped}
            </p>
          )}
          {job.error && <p className="error">{job.error}</p>}
          {job.report?.failures?.map((failure, index) => (
            <small className="error" key={index}>
              {failure}
            </small>
          ))}
        </div>
      )}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
type ManagedUser = User & { disabled?: boolean; locale?: string }
function UserManager({ current }: { current: User }) {
  const [items, setItems] = useState<ManagedUser[]>([]),
    [error, setError] = useState(''),
    [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/users/')
      .then((data) => {
        setItems(data.items || [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取用户失败'))
  useEffect(() => {
    refresh()
  }, [])
  const create = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const form = new FormData(e.currentTarget)
    try {
      const token = await csrf()
      await api('/api/admin/users/', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({
          username: form.get('username'),
          email: form.get('email'),
          password: form.get('password'),
          role: form.get('role'),
          locale: 'zh-CN',
        }),
      })
      e.currentTarget.reset()
      setNotice('用户已创建。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建用户失败')
    }
  }
  const save = async (user: ManagedUser, change: Record<string, unknown>) => {
    try {
      const token = await csrf()
      await api('/api/admin/users/' + encodeURIComponent(user.id), {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(change),
      })
      setNotice('用户已更新。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '更新用户失败')
    }
  }
  const reset = async (user: ManagedUser) => {
    const password = window.prompt('输入新密码（至少 12 位）：')
    if (!password) return
    try {
      const token = await csrf()
      await api('/api/admin/users/' + encodeURIComponent(user.id) + '/password', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ password }),
      })
      setNotice('密码已重置，旧会话已失效。')
    } catch (e) {
      setError(e instanceof Error ? e.message : '重置密码失败')
    }
  }
  const remove = async (user: ManagedUser) => {
    if (user.id === current.id) {
      setError('不能删除当前登录账户。')
      return
    }
    if (!window.confirm('删除用户 ' + user.username + '？')) return
    try {
      const token = await csrf()
      await api('/api/admin/users/' + encodeURIComponent(user.id), {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
      })
      setNotice('用户已删除。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除用户失败')
    }
  }
  return (
    <section className="users">
      <h2>用户</h2>
      <form className="user-create" onSubmit={create}>
        <input required name="username" placeholder="用户名" />
        <input required type="email" name="email" placeholder="邮箱" />
        <input
          required
          minLength={12}
          type="password"
          name="password"
          placeholder="初始密码（至少 12 位）"
        />
        <select name="role" defaultValue="author">
          <option value="admin">管理员</option>
          <option value="editor">编辑</option>
          <option value="author">作者</option>
          <option value="translator">译者</option>
        </select>
        <button>创建用户</button>
      </form>
      <div className="user-list">
        {items.map((user) => (
          <article key={user.id}>
            <div>
              <strong>{user.displayName || user.username}</strong>
              <small>
                {user.email} · {user.locale || '默认语言'}
                {user.disabled ? ' · 已停用' : ''}
              </small>
            </div>
            <div>
              <select
                aria-label={user.username + ' 的角色'}
                value={user.role}
                onChange={(e) => void save(user, { role: e.target.value })}
              >
                <option value="admin">管理员</option>
                <option value="editor">编辑</option>
                <option value="author">作者</option>
                <option value="translator">译者</option>
              </select>
              <button
                type="button"
                className="secondary"
                onClick={() => void save(user, { disabled: !user.disabled })}
              >
                {user.disabled ? '启用' : '停用'}
              </button>
              <button type="button" className="secondary" onClick={() => void reset(user)}>
                重置密码
              </button>
              <button type="button" className="danger" onClick={() => void remove(user)}>
                删除
              </button>
            </div>
          </article>
        ))}
      </div>
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
function TranslationTasks() {
  const [tasks, setTasks] = useState<TranslationTask[]>([])
  const [error, setError] = useState('')
  const [articleID, setArticleID] = useState('')
  const [target, setTarget] = useState('en')
  const refresh = () =>
    api('/api/admin/translations/tasks')
      .then((d) => {
        setTasks(d.items || [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取翻译任务失败'))
  useEffect(() => {
    refresh()
    const id = window.setInterval(refresh, 3000)
    return () => window.clearInterval(id)
  }, [])
  const enqueue = async (e: React.FormEvent) => {
    e.preventDefault()
    try {
      const token = await csrf()
      await api('/api/admin/translations/tasks', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ articleID, targetLocale: target }),
      })
      setArticleID('')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建失败')
    }
  }
  return (
    <section className="translation">
      <h2>翻译任务</h2>
      <p>仅发送被占位保护后的自然语言片段；代码、链接与公式不会传给模型。</p>
      <form className="taskform" onSubmit={enqueue}>
        <input
          required
          value={articleID}
          onChange={(e) => setArticleID(e.target.value)}
          placeholder="文章 ID"
        />
        <select value={target} onChange={(e) => setTarget(e.target.value)}>
          <option>en</option>
          <option>zh-TW</option>
          <option>ja</option>
          <option>de</option>
        </select>
        <button>开始翻译</button>
      </form>
      {error && <p className="error">{error}</p>}
      <div className="tasklist">
        {tasks.map((task) => (
          <article key={task.id}>
            <strong>
              {task.targetLocale} · {task.status}
            </strong>
            <span>
              {task.articleID} · {task.segmentsDone}/{task.segmentsTotal} 段 ·{' '}
              {(task.tokensIn || 0) + (task.tokensOut || 0)} tokens
            </span>
            {task.error && <small className="error">{task.error}</small>}
          </article>
        ))}
        {tasks.length === 0 && !error && <p>暂无翻译任务。</p>}
      </div>
    </section>
  )
}
function Dashboard({ user }: { user: User }) {
  const [stats, setStats] = useState<Record<string, number>>({}),
    [editing, setEditing] = useState('')
  useEffect(() => {
    api('/api/admin/dashboard')
      .then(setStats)
      .catch(() => {})
  }, [])
  return (
    <main className="dashboard">
      <aside>
        <strong>Mutiblog</strong>
        <a>仪表盘</a>
        <a>文章</a>
        <a>媒体</a>
        <a>翻译任务</a>
        <a>主题</a>
        <a>设置</a>
        <a>用户</a>
        <a>导入</a>
        <a>备份</a>
        <a>活动</a>
      </aside>
      <section>
        <h1>你好，{user.displayName || user.username}</h1>
        <p>角色：{user.role}</p>
        {editing ? (
          <PostEditor id={editing} done={() => setEditing('')} />
        ) : (
          <>
            <div className="stats">
              {Object.entries(stats).map(([key, value]) => (
                <div key={key}>
                  <strong>{value}</strong>
                  <span>{key}</span>
                </div>
              ))}
            </div>
            <Posts onEdit={setEditing} />
            <MediaManager />
            <TranslationTasks />
            <ThemeSettings />
            <SiteSettings />
            {user.role === 'admin' && <UserManager current={user} />}
            <ImportManager />
            <BackupManager />
            <SystemActivity />
          </>
        )}
      </section>
    </main>
  )
}
function App() {
  const [state, setState] = useState<'loading' | 'setup' | 'login' | 'app'>('loading')
  const [user, setUser] = useState<User | null>(null)
  const check = async () => {
    try {
      const status = await api('/api/auth/status')
      if (status.setupRequired) {
        setState('setup')
        return
      }
      const me = await api('/api/admin/me')
      setUser(me)
      setState('app')
    } catch {
      setState('login')
    }
  }
  useEffect(() => {
    check()
  }, [])
  if (state === 'loading') return <main className="card">加载中…</main>
  if (state === 'setup') return <Setup done={check} />
  if (state === 'login') return <Login done={check} />
  return <Dashboard user={user!} />
}
createRoot(document.getElementById('root')!).render(<App />)
