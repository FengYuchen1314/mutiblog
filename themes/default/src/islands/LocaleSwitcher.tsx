// 语言切换 island：写 preferred_locale cookie 后跳转目标语言。
import { useState } from 'react'

export default function LocaleSwitcher({
  alternates,
  locale,
}: {
  alternates?: Array<{ hrefLang?: string; href?: string }>
  locale?: string
}) {
  const options = (alternates || []).filter(
    (item) => item.hrefLang && item.hrefLang !== 'x-default',
  )
  const [value, setValue] = useState(locale || '')
  const change = (hrefLang: string, href: string) => {
    try {
      document.cookie = 'preferred_locale=' + hrefLang + '; path=/; max-age=31536000; samesite=lax'
    } catch {
      // 忽略 cookie 写入失败，仍允许跳转
    }
    if (href) window.location.assign(href)
  }
  return (
    <select
      className="locale-select"
      value={value}
      aria-label="Languages"
      onChange={(e) => {
        const option = options.find((item) => item.hrefLang === e.target.value)
        if (option?.hrefLang && option.href) change(option.hrefLang, option.href)
      }}
    >
      {options.map((item) => (
        <option key={item.hrefLang} value={item.hrefLang}>
          {item.hrefLang}
        </option>
      ))}
    </select>
  )
}
