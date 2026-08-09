// 页面骨架：内联主题样式 + 页头 + 正文 + 页脚。
import { themeSettings, type RenderProps } from '../lib/ctx'
import Header from './Header'
import Footer from './Footer'
import Island from '../lib/Island'
import mainCss from '../styles/main.css?inline'
import proseCss from '../styles/prose.css?inline'

export default function Layout({
  props,
  children,
  interactive = true,
}: {
  props: RenderProps
  children: React.ReactNode
  interactive?: boolean
}) {
  const settings = themeSettings(props)
  const accent =
    typeof settings.accentColor === 'string' &&
    /^#[0-9a-f]{3}(?:[0-9a-f]{3})?$/i.test(settings.accentColor)
      ? settings.accentColor
      : '#2563eb'
  const custom = String(settings.customCSS || '')
    .replace(/<\/?style/gi, '')
    .replace(/<script/gi, '')
    .replace(/javascript\s*:/gi, '')
    .replace(/expression\s*\(/gi, '')
  const css = `:root{--accent:${accent}}` + mainCss + '\n' + proseCss + '\n' + custom
  return (
    <>
      <style dangerouslySetInnerHTML={{ __html: css }} />
      <Header props={props} />
      {children}
      {interactive && (
        <>
          <Island name="ThemeToggle" hydrate="load" />
          <Island name="CopyCode" hydrate="idle" />
          <Island name="Lightbox" hydrate="visible" />
          <Island name="BackToTop" hydrate="visible" />
        </>
      )}
      <Footer />
    </>
  )
}
