// 客户端 island 引导：扫描 [data-island]，按策略动态加载并挂载。
import { createElement } from 'react'
import { createRoot } from 'react-dom/client'

type IslandComponent = {
  default: React.ComponentType<Record<string, unknown>>
}

const registry: Record<string, () => Promise<IslandComponent>> = {
  ThemeToggle: () => import('./islands/ThemeToggle'),
  LocaleSwitcher: () => import('./islands/LocaleSwitcher'),
  Search: () => import('./islands/Search'),
  CopyCode: () => import('./islands/CopyCode'),
  Lightbox: () => import('./islands/Lightbox'),
  Mermaid: () => import('./islands/Mermaid'),
  TocScrollSpy: () => import('./islands/TocScrollSpy'),
  BackToTop: () => import('./islands/BackToTop'),
}

const defaultStrategy: Record<string, 'load' | 'idle' | 'visible'> = {
  ThemeToggle: 'load',
  LocaleSwitcher: 'load',
  Search: 'idle',
  CopyCode: 'idle',
  Lightbox: 'visible',
  Mermaid: 'visible',
  TocScrollSpy: 'visible',
  BackToTop: 'visible',
}

function mount(name: string, node: HTMLElement) {
  const load = registry[name]
  if (!load) return
  let props: Record<string, unknown> = {}
  try {
    props = JSON.parse(node.dataset.islandProps || node.dataset.props || '{}')
  } catch {
    props = {}
  }
  load()
    .then((mod) => {
      const Component = mod.default
      createRoot(node).render(createElement(Component, props))
    })
    .catch((err) => console.error('island mount failed', name, err))
}

function schedule(name: string, node: HTMLElement, strategy: 'load' | 'idle' | 'visible') {
  if (strategy === 'load') {
    mount(name, node)
    return
  }
  if (strategy === 'idle') {
    if ('requestIdleCallback' in window) {
      window.requestIdleCallback(() => mount(name, node), { timeout: 2000 })
    } else {
      window.setTimeout(() => mount(name, node), 0)
    }
    return
  }
  const observer = new IntersectionObserver(
    (entries) => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          observer.disconnect()
          mount(name, node)
          break
        }
      }
    },
    { rootMargin: '200px' },
  )
  observer.observe(node)
}

document.querySelectorAll<HTMLElement>('[data-island]').forEach((node) => {
  const name = node.dataset.island || ''
  if (!name) return
  const strategy =
    (node.dataset.islandHydrate as 'load' | 'idle' | 'visible') || defaultStrategy[name] || 'idle'
  schedule(name, node, strategy)
})
