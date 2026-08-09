// 独立页面模板：标题与正文。
import Layout from '../components/Layout'
import Prose from '../components/Prose'
import LocaleSwitcher from '../components/LocaleSwitcher'
import type { RenderProps } from '../lib/ctx'

export default function Page(props: RenderProps) {
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind={props.kind}>
        <LocaleSwitcher props={props} />
        <article className="page-article">
          <h1>{props.title}</h1>
          <Prose html={props.html || ''} />
        </article>
      </main>
    </Layout>
  )
}
