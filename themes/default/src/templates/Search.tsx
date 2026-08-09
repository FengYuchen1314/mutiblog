// 搜索页模板：输入框与提示（搜索 island 在 T3/T5 接入）。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import { translate, type RenderProps } from '../lib/ctx'

export default function Search(props: RenderProps) {
  const t = (key: string) => translate(props, key)
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind={props.kind}>
        <LocaleSwitcher props={props} />
        <h1>{props.title}</h1>
        <label className="search-label" htmlFor="site-search">
          {props.title}
        </label>
        <input
          className="search-input"
          id="site-search"
          type="search"
          placeholder={t('site.search')}
          autoComplete="off"
        />
        <p className="search-hint">{t('site.search')}</p>
      </main>
    </Layout>
  )
}
