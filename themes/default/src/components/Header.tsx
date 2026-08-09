// 站点页头：品牌名 + 主导航。
import { siteURL, type RenderProps } from '../lib/ctx'

export default function Header({ props }: { props: RenderProps }) {
  const url = siteURL(props)
  return (
    <header className="site-header">
      <a className="site-brand" href={url.home()}>
        Mutiblog
      </a>
      <nav className="site-nav" aria-label="Main">
        <a href={url.home()}>首页</a>
        <a href={url.archive()}>归档</a>
        <a href={url.links()}>友链</a>
        <a href={url.search()}>搜索</a>
      </nav>
    </header>
  )
}
