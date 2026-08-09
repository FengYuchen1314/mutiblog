// 应用入口：认证状态机 + 路由挂载。
import { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from '@tanstack/react-router'
import { api } from './api/client'
import type { User } from './api/types'
import { UserProvider } from './components/UserContext'
import { router } from './router'
import SetupPage from './features/setup/SetupPage'
import LoginPage from './features/auth/LoginPage'
import './styles.css'

function App() {
  const [state, setState] = useState<'loading' | 'setup' | 'login' | 'app'>('loading')
  const [user, setUser] = useState<User | null>(null)
  const check = async () => {
    try {
      const status = await api('/api/auth/status')
      if (status.setupRequired) {
        setState('setup')
        return
      }
      const me = await api('/api/admin/me')
      setUser(me)
      setState('app')
    } catch {
      setState('login')
    }
  }
  useEffect(() => {
    void check()
  }, [])
  if (state === 'loading') return <main className="card">加载中…</main>
  if (state === 'setup') return <SetupPage done={check} />
  if (state === 'login') return <LoginPage done={check} />
  return (
    <UserProvider user={user}>
      <RouterProvider router={router} />
    </UserProvider>
  )
}

createRoot(document.getElementById('root')!).render(<App />)
