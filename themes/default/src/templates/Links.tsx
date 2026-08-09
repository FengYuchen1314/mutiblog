// 友链模板：按分组展示友链卡片。
import Layout from '../components/Layout'
import LocaleSwitcher from '../components/LocaleSwitcher'
import type { RenderProps } from '../lib/ctx'

type LinkGroup = {
  id?: string
  name?: string
  links?: Array<{
    name?: string
    url?: string
    logo?: string
    description?: string
  }>
}

export default function Links(props: RenderProps) {
  const groups = (props.groups || []) as LinkGroup[]
  return (
    <Layout props={props}>
      <main className="theme-default" data-kind="links">
        <LocaleSwitcher props={props} />
        <h1>{props.title}</h1>
        {groups.map((group) => (
          <section key={group.id} className="links-group">
            <h2>{group.name}</h2>
            <div className="links-grid">
              {(group.links || []).map((link, index) => (
                <a
                  key={index}
                  className="link-card"
                  href={link.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {link.logo && (
                    <img className="link-logo" src={link.logo} alt="" loading="lazy" />
                  )}
                  <strong>{link.name}</strong>
                  {link.description && <span>{link.description}</span>}
                </a>
              ))}
            </div>
          </section>
        ))}
      </main>
    </Layout>
  )
}
