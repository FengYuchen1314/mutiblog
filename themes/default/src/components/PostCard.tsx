// 文章卡片：标题链接 + 摘要。
export default function PostCard({
  item,
}: {
  item: { title?: string; url?: string; description?: string }
}) {
  const external = /^https?:\/\//.test(String(item.url))
  return (
    <article className="post-card">
      <h2 className="post-card-title">
        <a href={item.url} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
          {item.title}
        </a>
      </h2>
      {item.description && <p className="post-card-description">{item.description}</p>}
    </article>
  )
}
