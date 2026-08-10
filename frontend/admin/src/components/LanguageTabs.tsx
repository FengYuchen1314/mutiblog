// 语言标签栏：文章各语言版本状态一览与添加语言入口。
import { useState } from 'react'

export type LocaleInfo = {
  status?: string
  revision?: number
  manualEdited?: boolean
  translatedFrom?: number
  title?: string
  slug?: string
  published?: boolean
  date?: string
  author?: string
}

const TARGETS = ['en', 'zh-TW', 'ja', 'de']

function badge(locale: string, info: LocaleInfo | undefined, sourceRevision: number) {
  if (!info) return { label: '＋', cls: 'missing' }
  const status = info.status || 'missing'
  if (status === 'original') return { label: '原文', cls: 'original' }
  if (status === 'completed' && (info.translatedFrom || 0) < sourceRevision) {
    return { label: '⚠ 过期', cls: 'outdated' }
  }
  if (status === 'completed') return { label: '✓', cls: 'done' }
  if (status === 'manual') return { label: '✎ 人工', cls: 'manual' }
  if (status === 'pending' || status === 'translating') return { label: '⏳', cls: 'working' }
  if (status === 'failed') return { label: '✕', cls: 'failed' }
  return { label: '＋', cls: 'missing' }
}

export default function LanguageTabs({
  source,
  current,
  sourceRevision,
  versions,
  onSelect,
  onAdd,
}: {
  source: string
  current: string
  sourceRevision: number
  versions: Record<string, LocaleInfo>
  onSelect: (locale: string) => void
  onAdd: (locale: string, kind: 'blank' | 'copy' | 'translate') => void
}) {
  const [adding, setAdding] = useState('')
  const existing = new Set([source, ...Object.keys(versions)])
  const candidates = TARGETS.filter((locale) => !existing.has(locale))
  const locales = [...new Set([source, ...TARGETS, ...Object.keys(versions)])].filter(
    (locale) => versions[locale] || locale === source,
  )
  return (
    <div className="language-tabs" role="tablist" aria-label="语言版本">
      {locales.map((locale) => {
        const info = versions[locale]
        const mark = badge(locale, info, sourceRevision)
        return (
          <button
            key={locale}
            type="button"
            role="tab"
            aria-selected={locale === current}
            className={'lang-tab' + (locale === current ? ' active' : '')}
            onClick={() => onSelect(locale)}
          >
            {locale}
            {locale === source && <small>原文</small>}
            {locale !== source && <small className={'badge badge-' + mark.cls}>{mark.label}</small>}
          </button>
        )
      })}
      {candidates.length > 0 && (
        <span className="lang-add">
          <select value={adding} onChange={(e) => setAdding(e.target.value)} aria-label="添加语言">
            <option value="">＋ 添加语言</option>
            {candidates.map((locale) => (
              <option key={locale} value={locale}>
                {locale}
              </option>
            ))}
          </select>
          {adding && (
            <span className="lang-add-actions">
              <button type="button" onClick={() => onAdd(adding, 'translate')}>
                AI 翻译
              </button>
              <button type="button" onClick={() => onAdd(adding, 'copy')}>
                复制源文
              </button>
              <button type="button" onClick={() => onAdd(adding, 'blank')}>
                创建空白
              </button>
            </span>
          )}
        </span>
      )}
    </div>
  )
}
