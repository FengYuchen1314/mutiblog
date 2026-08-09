// 友链管理：分组看板、链接编辑与排序。
import { useEffect, useState } from 'react'
import { api, csrf, type API } from '../../api/client'

type Link = API['Link']
type LinkGroup = API['LinkGroup']

export default function LinkPage() {
  const [links, setLinks] = useState<Link[]>([])
  const [groups, setGroups] = useState<LinkGroup[]>([])
  const [editing, setEditing] = useState<Link | null>(null)
  const [groupName, setGroupName] = useState('')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const refresh = () =>
    Promise.all([api('/api/admin/links/'), api('/api/admin/links/groups')])
      .then(([linkData, groupData]) => {
        setLinks(linkData.items || [])
        setGroups(groupData.items || [])
      })
      .catch((e) => setError(e.message))
  useEffect(() => {
    refresh()
  }, [])
  const saveLink = async () => {
    if (!editing) return
    try {
      const token = await csrf()
      await api('/api/admin/links/' + encodeURIComponent(editing.id), {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(editing),
      })
      setNotice('友链已保存。')
      setEditing(null)
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存友链失败')
    }
  }
  const removeLink = async (link: Link) => {
    if (!window.confirm('删除友链 ' + link.name + '？')) return
    try {
      const token = await csrf()
      await api('/api/admin/links/' + encodeURIComponent(link.id), {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除友链失败')
    }
  }
  const moveLink = async (link: Link, group: string) => {
    try {
      const token = await csrf()
      await api('/api/admin/links/reorder', {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify([{ id: link.id, group, order: link.order || 0 }]),
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '移动友链失败')
    }
  }
  const saveGroup = async () => {
    if (!groupName.trim()) {
      setError('请输入分组名称。')
      return
    }
    try {
      const token = await csrf()
      const id = groupName
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9-]+/g, '-')
      const next = [...groups, { id, name: { 'zh-CN': groupName.trim() }, order: groups.length }]
      await api('/api/admin/links/groups', {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ groups: next }),
      })
      setGroupName('')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '创建分组失败')
    }
  }
  const removeGroup = async (group: LinkGroup) => {
    if (!window.confirm('删除分组 ' + group.name?.['zh-CN'] + '？组内友链将变为未分组。')) return
    try {
      const token = await csrf()
      const next = groups.filter((item) => item.id !== group.id)
      await api('/api/admin/links/groups', {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify({ groups: next }),
      })
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除分组失败')
    }
  }
  return (
    <section className="links-admin">
      <div className="activity-heading">
        <h2>友链</h2>
        <button
          type="button"
          onClick={() =>
            setEditing({ id: '', name: '', url: '', group: '', order: 0, description: {} })
          }
        >
          新建友链
        </button>
      </div>
      <div className="link-group-toolbar">
        <input
          value={groupName}
          onChange={(e) => setGroupName(e.target.value)}
          placeholder="新分组名称"
        />
        <button type="button" className="secondary" onClick={() => void saveGroup()}>
          新建分组
        </button>
      </div>
      {groups.map((group) => {
        const groupLinks = links.filter((link) => link.group === group.id)
        return (
          <div className="link-group-board" key={group.id}>
            <div className="activity-heading">
              <h3>{group.name?.['zh-CN'] || group.id}</h3>
              <button type="button" className="danger" onClick={() => void removeGroup(group)}>
                删除分组
              </button>
            </div>
            <div className="link-board-list">
              {groupLinks.map((link) => (
                <article key={link.id} className="link-board-item">
                  <div>
                    <strong>{link.name}</strong>
                    <small>{link.url}</small>
                  </div>
                  <div>
                    <select
                      value={link.group || ''}
                      onChange={(e) => void moveLink(link, e.target.value)}
                      aria-label="移动分组"
                    >
                      {groups.map((item) => (
                        <option key={item.id} value={item.id}>
                          {item.name?.['zh-CN'] || item.id}
                        </option>
                      ))}
                    </select>
                    <button
                      type="button"
                      className="secondary"
                      onClick={() => setEditing({ ...link })}
                    >
                      编辑
                    </button>
                    <button type="button" className="danger" onClick={() => void removeLink(link)}>
                      删除
                    </button>
                  </div>
                </article>
              ))}
              {groupLinks.length === 0 && <p>该分组暂无友链。</p>}
            </div>
          </div>
        )
      })}
      {editing && (
        <form
          className="link-form"
          onSubmit={(e) => {
            e.preventDefault()
            void saveLink()
          }}
        >
          <h3>{editing.id ? '编辑友链' : '新建友链'}</h3>
          <label>
            ID
            <input
              required
              value={editing.id}
              disabled={Boolean(editing.id)}
              onChange={(e) => setEditing({ ...editing, id: e.target.value })}
            />
          </label>
          <label>
            名称
            <input
              value={editing.name}
              onChange={(e) => setEditing({ ...editing, name: e.target.value })}
            />
          </label>
          <label>
            URL
            <input
              required
              value={editing.url || ''}
              onChange={(e) => setEditing({ ...editing, url: e.target.value })}
            />
          </label>
          <label>
            Logo
            <input
              value={editing.logo || ''}
              onChange={(e) => setEditing({ ...editing, logo: e.target.value })}
            />
          </label>
          <label>
            分组
            <select
              value={editing.group || ''}
              onChange={(e) => setEditing({ ...editing, group: e.target.value })}
            >
              {groups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name?.['zh-CN'] || group.id}
                </option>
              ))}
            </select>
          </label>
          <label>
            描述
            <textarea
              rows={2}
              value={editing.description?.['zh-CN'] || ''}
              onChange={(e) =>
                setEditing({
                  ...editing,
                  description: { ...(editing.description || {}), 'zh-CN': e.target.value },
                })
              }
            />
          </label>
          <div className="theme-actions">
            <button>保存</button>
            <button type="button" className="secondary" onClick={() => setEditing(null)}>
              取消
            </button>
          </div>
        </form>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
