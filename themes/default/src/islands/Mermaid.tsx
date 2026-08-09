// Mermaid island：visible 策略下懒加载 mermaid 并渲染为 SVG。
import { useEffect, useRef } from 'react'

export default function Mermaid({ code }: { code?: string }) {
  const host = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!host.current || !code) return
    let cancelled = false
    import('mermaid')
      .then(async (mod) => {
        const mermaid = mod.default
        mermaid.initialize({ startOnLoad: false, theme: 'default' })
        const id = 'mermaid-' + Math.random().toString(36).slice(2)
        const { svg } = await mermaid.render(id, code)
        if (!cancelled && host.current) host.current.innerHTML = svg
      })
      .catch((err) => console.error('mermaid render failed', err))
    return () => {
      cancelled = true
    }
  }, [code])
  return <div className="mermaid-rendered" ref={host} />
}
