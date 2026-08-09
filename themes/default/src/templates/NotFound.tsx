// 404 模板：错误信息与返回首页链接。
import Layout from '../components/Layout'
import { translate, siteURL, type RenderProps } from '../lib/ctx'

export default function NotFound(props: RenderProps) {
  const t = (key: string) => translate(props, key)
  const url = siteURL(props)
  return (
    <Layout props={props} interactive={false}>
      <main className="theme-default" data-kind={props.kind}>
        <h1>{props.title}</h1>
        <p>{props.message || t('site.pageNotFound')}</p>
        <p>
          <a href={url.home()}>{props.homeLabel || t('site.backToHome')}</a>
        </p>
      </main>
    </Layout>
  )
}
