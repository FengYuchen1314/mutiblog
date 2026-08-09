// 语言切换：渲染 alternates 中的语言链接，高亮当前语言。
import type { RenderProps } from '../lib/ctx'

export default function LocaleSwitcher({ props }: { props: RenderProps }) {
  const locales = (props.alternates || []).filter(
    (item) => item.hrefLang && item.hrefLang !== 'x-default',
  )
  if (!locales.length) return null
  return (
    <nav className="locale-switcher" aria-label="Languages">
      {locales.map((item) => (
        <a
          key={item.hrefLang}
          href={item.href}
          hreflang={item.hrefLang}
          aria-current={item.hrefLang === props.locale ? 'page' : undefined}
        >
          {item.hrefLang}
        </a>
      ))}
    </nav>
  )
}
