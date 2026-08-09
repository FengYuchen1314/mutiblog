// 文章页模板：标题、元信息与正文。
import Layout from '../components/Layout'
import Prose from '../components/Prose'
import LocaleSwitcher from '../components/LocaleSwitcher'
import { translate, type RenderProps } from '../lib/ctx'

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const y = date.getUTCFullYear()
  const m = String(date.getUTCMonth() + 1).padStart(2, '0')
  const d = String(date.getUTCDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

export default function Post(props: RenderProps) {
  const t = (key: string) => translate(props, key)
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind={props.kind}>
        <LocaleSwitcher props={props} />
        <article className="post-article">
          <header className="post-header">
            <h1>{props.title}</h1>
            {props.description && <p className="post-description">{props.description}</p>}
            <p className="post-meta">
              {props.author && <span>{props.author}</span>}
              {props.publishedAt && (
                <time dateTime={props.publishedAt}>
                  {t('site.publishedAt')} {formatDate(props.publishedAt)}
                </time>
              )}
              {props.modifiedAt && props.modifiedAt !== props.publishedAt && (
                <time dateTime={props.modifiedAt}>
                  {t('site.modifiedAt')} {formatDate(props.modifiedAt)}
                </time>
              )}
            </p>
          </header>
          <Prose html={props.html || ''} />
        </article>
      </main>
    </Layout>
  )
}
