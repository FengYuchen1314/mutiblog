// 集合页模板：首页 / 分类 / 标签 / 归档 / 友链共用（T8 拆分前）。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import PostCard from '../components/PostCard'
import Pagination from '../components/Pagination'
import type { RenderProps } from '../lib/ctx'

export default function Collection(props: RenderProps) {
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind={props.kind}>
        <LocaleSwitcher props={props} />
        <h1>{props.title}</h1>
        {props.description && <p className="collection-description">{props.description}</p>}
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
