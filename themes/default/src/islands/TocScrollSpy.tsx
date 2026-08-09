// 目录滚动高亮 island：从正文标题生成目录并跟踪当前章节。
import { useEffect, useState } from 'react'

type TocItem = { id: string; text: string }

export default function TocScrollSpy() {
  const [items, setItems] = useState<TocItem[]>([])
  const [active, setActive] = useState('')
  useEffect(() => {
    const headings = Array.from(document.querySelectorAll<HTMLElement>('.prose h2, .prose h3'))
      .map((heading) => ({ id: heading.id, text: heading.textContent || '' }))
      .filter((item) => item.id)
    setItems(headings)
    if (!headings.length) return
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) setActive((entry.target as HTMLElement).id)
        }
      },
      { rootMargin: '-80px 0px -70% 0px' },
    )
    headings.forEach((item) => {
      const element = document.getElementById(item.id)
      if (element) observer.observe(element)
    })
    return () => observer.disconnect()
  }, [])
  if (!items.length) return null
  return (
    <nav className="toc" aria-label="Table of contents">
      <strong>目录</strong>
      <ul>
        {items.map((item) => (
          <li key={item.id}>
            <a className={active === item.id ? 'active' : ''} href={'#' + item.id}>
              {item.text}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  )
}
