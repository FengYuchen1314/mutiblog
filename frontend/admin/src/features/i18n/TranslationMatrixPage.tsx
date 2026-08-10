// 多语言翻译矩阵：文章×语言状态一览、筛选、批量触发与成本确认。
import { useEffect, useMemo, useState } from 'react'
import { api, csrf, type API } from '../../api/client'

type PostSummary = API['PostSummary'] & { sourceRevision?: number }
type Task = API['TranslationTask']

const TARGET_LOCALES = ['en', 'zh-TW', 'ja', 'de']

type CellState = 'missing' | 'pending' | 'translating' | 'completed' | 'failed' | 'stale'

function cellState(post: PostSummary, locale: string, tasks: Task[]): CellState {
  const matches = tasks
    .filter((task) => task.articleID === post.id && task.targetLocale === locale)
    .sort((a, b) => Number(b.id || 0) - Number(a.id || 0))
  const latest = matches[0]
  if (!latest) return 'missing'
  if (
    latest.status === 'completed' &&
    latest.sourceRevision != null &&
    post.sourceRevision != null &&
    latest.sourceRevision < post.sourceRevision
  ) {
    return 'stale'
  }
  if (latest.status === 'completed') return 'completed'
  if (latest.status === 'failed') return 'failed'
  return 'pending'
}

const BADGE: Record<CellState, string> = {
  missing: '缺失',
  pending: '排队中',
  translating: '翻译中',
  completed: '✓',
  failed: '失败',
  stale: '⚠ 过期',
}

export default function TranslationMatrixPage() {
  const [posts, setPosts] = useState<PostSummary[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [filter, setFilter] = useState<'all' | 'missing' | 'stale' | 'failed'>('all')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [agreed, setAgreed] = useState(false)
  const [breaker, setBreaker] = useState<{ state: string; retryIn: number }>({
    state: 'closed',
    retryIn: 0,
  })
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const refresh = () =>
    Promise.all([
      api('/api/admin/posts/?locale=zh-CN&perPage=200'),
      api('/api/admin/translations/tasks'),
    ])
      .then(([postData, taskData]) => {
        setPosts(postData.items || [])
        setTasks(taskData.items || [])
        setError('')
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取翻译状态失败'))
  useEffect(() => {
    refresh()
    const timer = window.setInterval(refresh, 3000)
    return () => window.clearInterval(timer)
  }, [])
  useEffect(() => {
    const loadBreaker = () => {
      api('/api/admin/translations/breaker')
        .then((data) => setBreaker(data))
        .catch(() => {})
    }
    loadBreaker()
    const timer = window.setInterval(loadBreaker, 3000)
    return () => window.clearInterval(timer)
  }, [])
  const visible = useMemo(
    () =>
      posts.filter((post) => {
        if (filter === 'all') return true
        return TARGET_LOCALES.some((locale) => cellState(post, locale, tasks) === filter)
      }),
    [posts, tasks, filter],
  )
  const toggle = (id: string) => {
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }
  const taskCount = selected.size * TARGET_LOCALES.length
  const openConfirm = () => {
    if (taskCount === 0) {
      setError('请先选择文章。')
      return
    }
    setAgreed(false)
    setConfirmOpen(true)
  }
  const submitBatch = async () => {
    if (!agreed) return
    setConfirmOpen(false)
    let submitted = 0
    try {
      for (const id of selected) {
        for (const locale of TARGET_LOCALES) {
          const token = await csrf()
          await api('/api/admin/translations/tasks', {
            method: 'POST',
            headers: { 'X-CSRF-Token': token },
            body: JSON.stringify({ articleID: id, targetLocale: locale }),
          })
          submitted++
        }
      }
      setNotice('已提交 ' + submitted + ' 个翻译任务。')
      setSelected(new Set())
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '提交翻译失败')
    }
  }
  const resetBreaker = async () => {
    try {
      const token = await csrf()
      await api('/api/admin/translations/reset-breaker', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
      })
      setBreaker({ state: 'closed', retryIn: 0 })
      setNotice('熔断器已重置。')
    } catch (e) {
      setError(e instanceof Error ? e.message : '重置熔断器失败')
    }
  }
  const estimateTokens = taskCount * 4000
  const estimateMinutes = Math.ceil((taskCount * 45) / 60)
  return (
    <section className="matrix">
      <div className="activity-heading">
        <h2>翻译矩阵</h2>
        <div className="matrix-filters">
          {(['all', 'missing', 'stale', 'failed'] as const).map((value) => (
            <button
              key={value}
              type="button"
              className={filter === value ? 'selected' : 'secondary'}
              onClick={() => setFilter(value)}
            >
              {value === 'all' ? '全部' : BADGE[value]}
            </button>
          ))}
        </div>
        <button type="button" onClick={openConfirm}>
          批量翻译（{selected.size} 篇）
        </button>
      </div>
      {error && <p className="error">{error}</p>}
      {notice && <p className="notice">{notice}</p>}
      {breaker.state !== 'closed' && (
        <div className="breaker-banner">
          <span>
            AI 服务熔断中（{breaker.state}）
            {breaker.retryIn > 0 ? ' · ' + breaker.retryIn + ' 秒后可重试' : ''}
          </span>
          <button type="button" className="secondary" onClick={() => void resetBreaker()}>
            立即重试
          </button>
        </div>
      )}
      <div className="matrix-table">
        <table>
          <thead>
            <tr>
              <th />
              <th>文章</th>
              {TARGET_LOCALES.map((locale) => (
                <th key={locale}>{locale}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {visible.map((post) => (
              <tr key={post.id}>
                <td>
                  <input
                    type="checkbox"
                    checked={selected.has(post.id)}
                    onChange={() => toggle(post.id)}
                    aria-label="选择文章"
                  />
                </td>
                <td>
                  <strong>{post.title}</strong>
                  <small>/{post.slug}</small>
                </td>
                {TARGET_LOCALES.map((locale) => {
                  const state = cellState(post, locale, tasks)
                  return (
                    <td key={locale}>
                      <span className={'badge badge-' + state}>{BADGE[state]}</span>
                    </td>
                  )
                })}
              </tr>
            ))}
            {visible.length === 0 && (
              <tr>
                <td colSpan={TARGET_LOCALES.length + 2}>没有符合条件的文章。</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      {confirmOpen && (
        <div className="confirm-overlay">
          <div className="confirm-box">
            <h3>确认批量翻译</h3>
            <p>
              {selected.size} 篇文章 × {TARGET_LOCALES.length} 种语言 = {taskCount} 个任务
            </p>
            <p>预计消耗：约 {estimateTokens.toLocaleString()} tokens</p>
            <p>预计耗时：约 {estimateMinutes} 分钟</p>
            <p className="warn">⚠ 这会产生实际的 API 费用，请确认你的用量额度。</p>
            <label className="check">
              <input
                type="checkbox"
                checked={agreed}
                onChange={(e) => setAgreed(e.target.checked)}
              />
              我已了解费用
            </label>
            <div className="theme-actions">
              <button type="button" disabled={!agreed} onClick={() => void submitBatch()}>
                开始翻译
              </button>
              <button type="button" className="secondary" onClick={() => setConfirmOpen(false)}>
                取消
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  )
}
