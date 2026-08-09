// 内容导入：上传 Markdown 或 ZIP，支持 dry-run 与自动建分类。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'
import type { ImportResult } from '../../api/types'

export default function ImportPage() {
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
