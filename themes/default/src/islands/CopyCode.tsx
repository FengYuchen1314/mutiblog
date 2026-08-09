// 代码复制 island：为 pre.shiki 注入复制按钮（idle 策略）。
import { useEffect } from 'react'

export default function CopyCode() {
  useEffect(() => {
    const pres = document.querySelectorAll('pre.shiki')
    for (const pre of pres) {
      if (pre.querySelector('.copy-code-btn')) continue
      const button = document.createElement('button')
      button.type = 'button'
      button.className = 'copy-code-btn'
      button.textContent = '复制'
      button.addEventListener('click', async () => {
        const code = pre.querySelector('code')?.innerText || ''
        try {
          await navigator.clipboard.writeText(code)
          button.textContent = '已复制'
        } catch {
          button.textContent = '复制失败'
        }
        setTimeout(() => {
          button.textContent = '复制'
        }, 1500)
      })
      pre.appendChild(button)
    }
  }, [])
  return null
}
