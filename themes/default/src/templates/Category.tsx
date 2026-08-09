// 分类模板：面包屑 + 描述 + 子分类 + 文章列表。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import PostCard from '../components/PostCard'
import type { RenderProps } from '../lib/ctx'

export default function Category(props: RenderProps) {
  const category = (props.category || {}) as Record<string, unknown>
  const breadcrumb = (props.breadcrumb || []) as Array<{ name?: string; url?: string }>
  const children = (props.children || []) as Array<{
    title?: string
    url?: string
    description?: string
  }>
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind="category">
        <LocaleSwitcher props={props} />
        {breadcrumb.length > 0 && (
          <nav className="breadcrumb" aria-label="Breadcrumb">
            {breadcrumb.map((item, index) => (
              <span key={index}>
                {index > 0 && <span className="breadcrumb-sep"> / </span>}
                <a href={item.url}>{item.name}</a>
              </span>
            ))}
          </nav>
        )}
        <h1>{props.title}</h1>
        {typeof category.description === 'string' && category.description && (
          <p className="collection-description">{category.description}</p>
        )}
        {children.length > 0 && (
          <div className="post-grid">
            {children.map((child, index) => (
              <PostCard key={index} item={child} />
            ))}
          </div>
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
