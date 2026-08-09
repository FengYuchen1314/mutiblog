// 页面与组件共享的领域类型。
import type { API } from './client'

export type User = API['User']
export type Post = API['PostSummary']
export type TranslationTask = API['TranslationTask']
export type MediaItem = API['MediaItem']

export type EditorPost = {
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

export type ThemeField = {
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

export type ThemeItem = {
  manifest: { name: string; displayName?: Record<string, string> }
  schema: { fields?: ThemeField[] }
}

export type LogEntry = {
  id: number
  level?: string
  component?: string
  message?: string
  actor?: string
  action?: string
  createdAt: string
}

export type BackupItem = { path: string; size: number; createdAt: string }

export type ImportResult = {
  jobId: number
  status: string
  phase: string
  report?: { found: number; imported: number; skipped: number; failures?: string[] }
  error?: string
}

export type ManagedUser = User & { disabled?: boolean; locale?: string }
