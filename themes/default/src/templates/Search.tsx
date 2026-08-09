// 搜索页模板：输入框与提示（搜索 island 在 T3/T5 接入）。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import { translate, type RenderProps } from '../lib/ctx'
import Island from '../lib/Island'

export default function Search(props: RenderProps) {
  const t = (key: string) => translate(props, key)
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind={props.kind}>
        <Island
          name="LocaleSwitcher"
          hydrate="load"
          props={{ alternates: props.alternates, locale: props.locale }}
        >
          <LocaleSwitcher props={props} />
        </Island>
        <h1>{props.title}</h1>
        <Island
          name="Search"
          hydrate="idle"
          props={{ indexURL: props.searchIndexURL, placeholder: t('site.search') }}
        >
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
        </Island>
      </main>
    </Layout>
  )
}
