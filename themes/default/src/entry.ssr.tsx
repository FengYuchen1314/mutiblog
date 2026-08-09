// SSR 入口：导出渲染器可用的 templates（home/category/tag/archive/links 为 T8 拆分后的 kind）。
import Home from './templates/Home'
import Post from './templates/Post'
import Page from './templates/Page'
import Category from './templates/Category'
import Tag from './templates/Tag'
import Archive from './templates/Archive'
import Links from './templates/Links'
import Collection from './templates/Collection'
import Search from './templates/Search'
import NotFound from './templates/NotFound'

export const templates = {
  home: Home,
  post: Post,
  page: Page,
  category: Category,
  tag: Tag,
  archive: Archive,
  links: Links,
  collection: Collection,
  search: Search,
  not_found: NotFound,
} as const
