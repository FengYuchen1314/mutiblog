// 翻译任务：查看任务列表并创建新翻译任务。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'
import type { TranslationTask } from '../../api/types'

export default function TranslationTasksPage() {
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
