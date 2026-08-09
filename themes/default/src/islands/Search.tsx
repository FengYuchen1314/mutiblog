// 搜索 island：首次输入时才拉取索引并用 MiniSearch 检索正文。
import { useEffect, useRef, useState } from 'react'
import MiniSearch from 'minisearch'

type SearchDoc = {
  i?: string
  t?: string
  d?: string
  u?: string
  p?: string
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
  const engineRef = useRef<MiniSearch<SearchDoc> | null>(null)
  useEffect(() => {
    let cancelled = false
    if (!query.trim()) {
      setResults([])
      return
    }
    const run = async () => {
      if (!engineRef.current && indexURL) {
        const docs = await fetch(indexURL)
          .then((r) => (r.ok ? r.json() : []))
          .catch(() => [])
        const engine = new MiniSearch<SearchDoc>({
          fields: ['t', 'd', 'p'],
          storeFields: ['i', 't', 'd', 'u', 'p'],
          searchOptions: { prefix: true, fuzzy: 0.2 },
        })
        engine.addAll(docs)
        engineRef.current = engine
      }
      if (cancelled) return
      const q = query.trim().toLowerCase()
      const found = engineRef.current ? engineRef.current.search(q) : []
      setResults(found.map((hit) => hit as unknown as SearchDoc).slice(0, 20))
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
            <a href={doc.u}>{doc.t}</a>
            {doc.d && <p>{doc.d}</p>}
          </li>
        ))}
      </ul>
    </div>
  )
}
