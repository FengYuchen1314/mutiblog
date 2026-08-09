// 归档模板：all 视图为年份时间线，month 视图为当月文章列表。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import PostCard from '../components/PostCard'
import type { RenderProps } from '../lib/ctx'

type ArchiveYear = {
  year?: number
  count?: number
  months?: Array<{ month?: number; count?: number; url?: string }>
}

export default function Archive(props: RenderProps) {
  const scope = String(props.scope || 'all')
  const years = (props.years || []) as ArchiveYear[]
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind="archive">
        <LocaleSwitcher props={props} />
        <h1>{props.title}</h1>
        {scope === 'all' && (
          <div className="archive-timeline">
            {years.map((year) => (
              <section key={year.year} className="archive-year">
                <h2>
                  {year.year}
                  <small>{year.count} 篇</small>
                </h2>
                <ul>
                  {(year.months || []).map((month) => (
                    <li key={month.month}>
                      <a href={month.url}>
                        {year.year}-{String(month.month || 0).padStart(2, '0')}
                      </a>
                      <span>{month.count} 篇</span>
                    </li>
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
        {scope === 'month' && (
          <div className="post-grid">
            {(props.items || []).map((item, index) => (
              <PostCard key={index} item={item} />
            ))}
          </div>
        )}
      </main>
    </Layout>
  )
}
