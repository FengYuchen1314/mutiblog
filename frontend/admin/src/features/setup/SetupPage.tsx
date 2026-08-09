// 首次安装向导：创建第一位管理员账号。
import { useState } from 'react'
import { api } from '../../api/client'

export default function SetupPage({ done }: { done: () => void }) {
  const [error, setError] = useState('')
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    try {
      await api('/api/auth/setup', {
        method: 'POST',
        body: JSON.stringify({
          username: f.get('username'),
          email: f.get('email'),
          password: f.get('password'),
          locale: 'zh-CN',
        }),
      })
      done()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Setup failed')
    }
  }
  return (
    <main className="card">
      <h1>设置 Mutiblog</h1>
      <p>创建第一位管理员以开始管理站点。</p>
      <form onSubmit={submit}>
        <input required name="username" placeholder="用户名" />
        <input required type="email" name="email" placeholder="邮箱" />
        <input
          required
          minLength={12}
          type="password"
          name="password"
          placeholder="密码（至少 12 位）"
        />
        <button>创建管理员</button>
        {error && <p className="error">{error}</p>}
      </form>
    </main>
  )
}
