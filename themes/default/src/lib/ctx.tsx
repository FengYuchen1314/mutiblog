// 渲染辅助：从渲染器传入的扁平 props 派生站点上下文。
// 注意：SSR 模板是纯函数（渲染器直接以 createElement 渲染模板并传入 props），
// 因此这里不提供 React hooks，而是返回纯辅助函数，保证与渲染器解耦。

export type RenderProps = {
  kind: string
  title?: string
  description?: string
  body?: string
  html?: string
  locale?: string
  author?: string
  publishedAt?: string
  modifiedAt?: string
  theme?: Record<string, unknown>
  themeName?: string
  themeDir?: string
  markdown?: Record<string, unknown>
  canonical?: string
  alternates?: Array<{ hrefLang?: string; href?: string }>
  items?: Array<{ title?: string; url?: string; description?: string }>
  pagination?: {
    total?: number
    links?: Array<{ page?: number | string; url?: string; current?: boolean }>
  }
  searchIndexURL?: string
  message?: string
  homeLabel?: string
  prefix?: string
  [key: string]: unknown
}

/** 读取主题设置（settings.schema.json 的值）。 */
export function themeSettings(props: RenderProps): Record<string, unknown> {
  return props.theme || {}
}

const COPY: Record<string, string> = {
  'site.readMore': '阅读全文',
  'site.publishedAt': '发布于',
  'site.modifiedAt': '更新于',
  'site.home': '首页',
  'site.search': '搜索',
  'site.backToHome': '返回首页',
  'site.pageNotFound': '页面未找到。',
}

/** 取主题文案；未内置的 key 原样返回。 */
export function translate(_props: RenderProps, key: string): string {
  return COPY[key] || key
}

/** 站点链接构造器：自动带当前语言前缀。 */
export function siteURL(props: RenderProps) {
  const prefix = props.prefix || String(props.locale || 'en').toLowerCase()
  const root = '/' + prefix
  return {
    home: () => root + '/',
    post: (slug: string) => root + '/posts/' + slug + '/',
    page: (slug: string) => root + '/' + slug + '/',
    search: () => root + '/search/',
    category: (slug: string) => root + '/categories/' + slug + '/',
    tag: (slug: string) => root + '/tags/' + slug + '/',
    archive: () => root + '/archives/',
    links: () => root + '/links/',
  }
}
