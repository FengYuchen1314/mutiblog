// 搜索 island：首次输入时才拉取索引并做子串匹配（T5 升级为 MiniSearch）。
import { useEffect, useRef, useState } from 'react'

type SearchDoc = {
  title?: string
  url?: string
  description?: string
}

export default function Search({
  indexURL,
  placeholder,
}: {
  indexURL?: string
  placeholder?: string
}) {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchDoc[]>([])
  const indexRef = useRef<SearchDoc[] | null>(null)
  useEffect(() => {
    let cancelled = false
    if (!query.trim()) {
      setResults([])
      return
    }
    const run = async () => {
      if (!indexRef.current && indexURL) {
        indexRef.current = await fetch(indexURL)
          .then((r) => (r.ok ? r.json() : []))
          .catch(() => [])
      }
      if (cancelled) return
      const q = query.trim().toLowerCase()
      const found = (indexRef.current || [])
        .filter((doc) =>
          String((doc.title || '') + ' ' + (doc.description || ''))
            .toLowerCase()
            .includes(q),
        )
        .slice(0, 20)
      setResults(found)
    }
    void run()
    return () => {
      cancelled = true
    }
  }, [query, indexURL])
  return (
    <div className="search-island">
      <input
        className="search-input"
        type="search"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder={placeholder}
        autoComplete="off"
      />
      <ul className="search-results">
        {results.map((doc, index) => (
          <li key={index}>
            <a href={doc.url}>{doc.title}</a>
            {doc.description && <p>{doc.description}</p>}
          </li>
        ))}
      </ul>
    </div>
  )
}
