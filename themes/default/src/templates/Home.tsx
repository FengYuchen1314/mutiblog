// 首页模板：置顶区 + 文章流 + 分页。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import PostCard from '../components/PostCard'
import Pagination from '../components/Pagination'
import type { RenderProps } from '../lib/ctx'

export default function Home(props: RenderProps) {
  const pinned = (props.pinned || []) as Array<{
    title?: string
    url?: string
    description?: string
  }>
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind="home">
        <LocaleSwitcher props={props} />
        <h1>{props.title}</h1>
        {pinned.length > 0 && (
          <section className="pinned" aria-label="置顶文章">
            <h2>置顶</h2>
            <div className="post-grid">
              {pinned.map((item, index) => (
                <PostCard key={index} item={item} />
              ))}
            </div>
          </section>
        )}
        <div className="post-grid">
          {(props.items || []).map((item, index) => (
            <PostCard key={index} item={item} />
          ))}
        </div>
        <Pagination pagination={props.pagination} />
      </main>
    </Layout>
  )
}
