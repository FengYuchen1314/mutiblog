// 仪表盘：统计卡片 + 当前用户信息。
import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import { useUser } from '../../components/UserContext'

export default function DashboardPage() {
  const user = useUser()
  const [stats, setStats] = useState<Record<string, number>>({})
  useEffect(() => {
    api('/api/admin/dashboard')
      .then(setStats)
      .catch(() => {})
  }, [])
  return (
    <section>
      <h1>你好，{user?.displayName || user?.username}</h1>
      <p>角色：{user?.role}</p>
      <div className="stats">
        {Object.entries(stats).map(([key, value]) => (
          <div key={key}>
            <strong>{value}</strong>
            <span>{key}</span>
          </div>
        ))}
      </div>
    </section>
  )
}
