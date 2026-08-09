// 深浅色切换 island：切换 html.dark 并写入 localStorage。
import { useState } from 'react'

export default function ThemeToggle() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains('dark'))
  const toggle = () => {
    const next = !dark
    setDark(next)
    document.documentElement.classList.toggle('dark', next)
    document.documentElement.classList.toggle('light', !next)
    try {
      localStorage.setItem('theme', next ? 'dark' : 'light')
    } catch {
      // 隐私模式下忽略写入失败
    }
  }
  return (
    <button type="button" className="theme-toggle" onClick={toggle} aria-label="切换主题">
      {dark ? '☀️' : '🌙'}
    </button>
  )
}
