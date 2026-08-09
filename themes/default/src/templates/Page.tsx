// 独立页面模板：标题与正文。
import Layout from '../components/Layout'
import Prose from '../components/Prose'
import LocaleSwitcher from '../components/LocaleSwitcher'
import type { RenderProps } from '../lib/ctx'
import Island from '../lib/Island'

export default function Page(props: RenderProps) {
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
        <article className="page-article">
          <h1>{props.title}</h1>
          <Prose html={props.html || ''} />
        </article>
      </main>
    </Layout>
  )
}
