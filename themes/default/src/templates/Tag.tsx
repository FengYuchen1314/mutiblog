// 标签模板：描述 + 文章列表。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import PostCard from '../components/PostCard'
import type { RenderProps } from '../lib/ctx'

export default function Tag(props: RenderProps) {
  const tag = (props.tag || {}) as Record<string, unknown>
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind="tag">
        <LocaleSwitcher props={props} />
        <h1>{props.title}</h1>
        {typeof tag.description === 'string' && tag.description && (
          <p className="collection-description">{tag.description}</p>
        )}
        <div className="post-grid">
          {(props.items || []).map((item, index) => (
            <PostCard key={index} item={item} />
          ))}
        </div>
      </main>
    </Layout>
  )
}
