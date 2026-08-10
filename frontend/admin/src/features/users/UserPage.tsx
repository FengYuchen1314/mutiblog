// 用户管理：创建用户、改角色、停用、重置密码与删除。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'
import type { ManagedUser } from '../../api/types'
import { useUser } from '../../components/UserContext'

export default function UserPage() {
  const current = useUser()
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
    const target = e.currentTarget
    const form = new FormData(target)
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
      target.reset()
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
    if (user.id === current?.id) {
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
