// 分页导航：渲染分页链接与当前页标记。
export default function Pagination({
  pagination,
}: {
  pagination?: {
    total?: number
    links?: Array<{ page?: number | string; url?: string; current?: boolean }>
  }
}) {
  if (!pagination || (pagination.total || 0) <= 1) return null
  return (
    <nav aria-label="Pagination">
      <ol className="pagination">
        {(pagination.links || []).map((link) => (
          <li key={String(link.page) + (link.url || '')}>
            {link.current ? (
              <span aria-current="page">{link.page}</span>
            ) : (
              <a href={link.url}>{link.page}</a>
            )}
          </li>
        ))}
      </ol>
    </nav>
  )
}
