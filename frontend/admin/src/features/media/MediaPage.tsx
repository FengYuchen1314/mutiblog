// 媒体库：上传、建目录、浏览与删除。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'
import type { MediaItem } from '../../api/types'

export default function MediaPage() {
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
    const target = e.currentTarget
    const form = new FormData(target)
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
      target.reset()
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
