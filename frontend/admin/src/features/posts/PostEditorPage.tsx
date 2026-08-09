// 文章编辑器：加载、自动保存、预览、发布与修订列表。
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { api, csrf } from '../../api/client'
import type { EditorPost } from '../../api/types'
import MarkdownEditor from '../../components/MarkdownEditor'

export default function PostEditorPage({ id }: { id: string }) {
  const navigate = useNavigate()
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
        <button type="button" onClick={() => navigate({ to: '/posts' })}>
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
        <button type="button" onClick={() => navigate({ to: '/posts' })}>
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
