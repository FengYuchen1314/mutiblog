// SSR 入口：导出渲染器可用的 templates（键为渲染器实际发送的 5 种 kind）。
import Post from './templates/Post'
import Page from './templates/Page'
import Collection from './templates/Collection'
import Search from './templates/Search'
import NotFound from './templates/NotFound'

export const templates = {
  post: Post,
  page: Page,
  collection: Collection,
  search: Search,
  not_found: NotFound,
} as const
