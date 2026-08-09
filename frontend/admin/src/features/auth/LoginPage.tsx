// 登录页：提交账号密码并回调登录成功。
import { useState } from 'react'
import { api } from '../../api/client'

export default function LoginPage({ done }: { done: () => void }) {
  const [error, setError] = useState('')
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    try {
      await api('/api/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username: f.get('username'), password: f.get('password') }),
      })
      done()
    } catch (e) {
      setError(e instanceof Error ? e.message : '登录失败')
    }
  }
  return (
    <main className="card">
      <h1>登录 Mutiblog</h1>
      <form onSubmit={submit}>
        <input required name="username" placeholder="用户名" />
        <input required type="password" name="password" placeholder="密码" />
        <button>登录</button>
        {error && <p className="error">{error}</p>}
      </form>
    </main>
  )
}
