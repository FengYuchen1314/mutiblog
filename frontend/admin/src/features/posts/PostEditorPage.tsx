// 文章编辑器：多语言标签栏、自动保存、预览、发布与修订列表。
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { api, csrf } from '../../api/client'
import type { EditorPost } from '../../api/types'
import MarkdownEditor from '../../components/MarkdownEditor'
import LanguageTabs, { type LocaleInfo } from '../../components/LanguageTabs'

type LocaleMatrix = {
  source: string
  sourceRevision: number
  versions: Record<string, LocaleInfo>
}

const SOURCE_LOCK = '🔒'

export default function PostEditorPage({ id, locale }: { id: string; locale: string }) {
  const navigate = useNavigate()
  const [post, setPost] = useState<EditorPost | null>(null)
  const [matrix, setMatrix] = useState<LocaleMatrix | null>(null)
  const [error, setError] = useState('')
  const [preview, setPreview] = useState('')
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [revisions, setRevisions] = useState<number[]>([])
  const [manualConfirm, setManualConfirm] = useState(false)
  const [skipManualConfirm, setSkipManualConfirm] = useState(false)
  const [title, setTitle] = useState('')
  const [slug, setSlug] = useState('')
  const [description, setDescription] = useState('')
  const [body, setBody] = useState('')
  const [categories, setCategories] = useState('')
  const [tags, setTags] = useState('')
  const loaded = useRef(false)
  const localKey = 'mutiblog:draft:' + id + ':' + locale

  const load = () =>
    api('/api/admin/posts/' + id + '?locale=' + encodeURIComponent(locale))
      .then(async (value: EditorPost) => {
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
        try {
          const matrixData = await api('/api/admin/posts/' + id + '/locales')
          setMatrix(matrixData)
        } catch {
          setMatrix(null)
        }
      })
      .catch((e) => setError(e.message))

  useEffect(() => {
    loaded.current = false
    void load()
  }, [id, locale])

  const payload = () => ({
    locale,
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

  const requestSave = async () => {
    const derived = matrix != null && locale !== matrix.source
    const skip = localStorage.getItem('mutiblog:skipManualConfirm') === '1'
    if (derived && !skip) {
      setManualConfirm(true)
      return
    }
    await save(false)
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
    api('/api/admin/posts/' + id + '/revisions?locale=' + encodeURIComponent(locale))
      .then((value) =>
        setRevisions((value.items || []).map((item: any) => Number(item.revision || item))),
      )
      .catch((e) => setError(e.message))

  const addLocale = async (target: string, kind: 'blank' | 'copy' | 'translate') => {
    try {
      const token = await csrf()
      await api('/api/admin/posts/' + id + '/locales/' + encodeURIComponent(target), {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ source: kind }),
      })
      await load()
      if (kind === 'translate') {
        navigate({ to: '/posts/$id', params: { id }, search: { locale: target } })
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : '添加语言失败')
    }
  }

  const updateTranslation = async () => {
    try {
      const token = await csrf()
      await api('/api/admin/translations/tasks', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ articleID: id, targetLocale: locale, force: true }),
      })
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '更新翻译失败')
    }
  }

  useEffect(() => {
    const working =
      matrix &&
      Object.values(matrix.versions).some(
        (info) => info.status === 'pending' || info.status === 'translating',
      )
    if (!working) return
    const timer = window.setInterval(() => {
      api('/api/admin/posts/' + id + '/locales')
        .then(setMatrix)
        .catch(() => {})
    }, 3000)
    return () => window.clearInterval(timer)
  }, [matrix, id])

  const sourceInfo = matrix?.versions[matrix.source]
  const derived = matrix != null && locale !== matrix.source
  const currentInfo = matrix?.versions[locale]
  const outdated =
    currentInfo?.status === 'completed' &&
    (currentInfo.translatedFrom || 0) < (matrix?.sourceRevision || 0)

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
        <button type="button" onClick={() => void requestSave()}>
          保存
        </button>
        <button type="button" onClick={() => void publish()}>
          发布
        </button>
      </div>
      {matrix && (
        <LanguageTabs
          source={matrix.source}
          current={locale}
          sourceRevision={matrix.sourceRevision}
          versions={matrix.versions}
          onSelect={(target) =>
            navigate({ to: '/posts/$id', params: { id }, search: { locale: target } })
          }
          onAdd={addLocale}
        />
      )}
      {outdated && (
        <div className="outdated-banner">
          <span>
            源文章已更新到版本 {matrix?.sourceRevision}，译文基于版本 {currentInfo?.translatedFrom}
          </span>
          <button type="button" className="secondary" onClick={() => void updateTranslation()}>
            更新翻译
          </button>
        </div>
      )}
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
            发布时间（由源语言控制）
            <input
              readOnly
              value={sourceInfo?.date ? String(sourceInfo.date).slice(0, 16) : ''}
              title="由源语言（' + matrix?.source + '）控制"
            />
          </label>
          <label>
            作者（由源语言控制）
            <input readOnly value={sourceInfo?.author || ''} title="由源语言控制" />
          </label>
          <label>
            分类
            {derived && (
              <span className="lock" title={'由源语言（' + matrix.source + '）控制'}>
                {SOURCE_LOCK}
              </span>
            )}
            <input value={categories} disabled={derived} onChange={change(setCategories)} />
          </label>
          <label>
            标签
            {derived && (
              <span className="lock" title={'由源语言（' + matrix.source + '）控制'}>
                {SOURCE_LOCK}
              </span>
            )}
            <input value={tags} disabled={derived} onChange={change(setTags)} />
          </label>
          <button type="button" onClick={loadRevisions}>
            查看修订
          </button>
          {revisions.length > 0 && <p>可用修订：{revisions.join(', ')}</p>}
        </aside>
      </div>
      {preview && <article className="preview" dangerouslySetInnerHTML={{ __html: preview }} />}
      {manualConfirm && (
        <div className="confirm-overlay">
          <div className="confirm-box">
            <h3>保存派生语言</h3>
            <p>保存后此译文将不再被 AI 自动更新。</p>
            <label className="check">
              <input
                type="checkbox"
                checked={skipManualConfirm}
                onChange={(e) => setSkipManualConfirm(e.target.checked)}
              />
              不再提示
            </label>
            <div className="theme-actions">
              <button
                type="button"
                onClick={() => {
                  if (skipManualConfirm) {
                    localStorage.setItem('mutiblog:skipManualConfirm', '1')
                  }
                  setManualConfirm(false)
                  void save(false)
                }}
              >
                保存
              </button>
              <button type="button" className="secondary" onClick={() => setManualConfirm(false)}>
                取消
              </button>
            </div>
          </div>
        </div>
      )}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
