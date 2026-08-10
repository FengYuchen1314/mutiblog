// 后台路由树：每个管理页面对应一个 URL，刷新后保持当前位置。
import { createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import Layout from './components/Layout'
import DashboardPage from './features/dashboard/DashboardPage'
import PostListPage from './features/posts/PostListPage'
import PostEditorPage from './features/posts/PostEditorPage'
import MediaPage from './features/media/MediaPage'
import TranslationMatrixPage from './features/i18n/TranslationMatrixPage'
import ThemeSettingsPage from './features/settings/ThemeSettingsPage'
import SiteSettingsPage from './features/settings/SiteSettingsPage'
import UserPage from './features/users/UserPage'
import CategoryPage from './features/categories/CategoryPage'
import TagPage from './features/tags/TagPage'
import MenuPage from './features/menus/MenuPage'
import LinkPage from './features/links/LinkPage'
import PagesPage from './features/pages/PagesPage'
import TrashPage from './features/system/TrashPage'
import LogPage from './features/system/LogPage'
import ImportPage from './features/system/ImportPage'
import BackupPage from './features/system/BackupPage'
import SystemActivityPage from './features/system/SystemActivityPage'

const rootRoute = createRootRoute({ component: Layout })

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  component: DashboardPage,
})

const postsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/posts',
  component: PostListPage,
})

const postEditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/posts/$id',
  component: () => {
    const { id } = postEditRoute.useParams()
    const search = postEditRoute.useSearch()
    const locale = (search.locale as string | undefined) || 'zh-CN'
    return <PostEditorPage id={id} locale={locale} />
  },
})

const mediaRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/media',
  component: MediaPage,
})

const translationsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/translations',
  component: TranslationMatrixPage,
})

const themesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/themes',
  component: ThemeSettingsPage,
})

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/settings',
  component: SiteSettingsPage,
})

const usersRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/users',
  component: UserPage,
})

const categoriesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/categories',
  component: CategoryPage,
})

const tagsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/tags',
  component: TagPage,
})

const menusRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/menus',
  component: MenuPage,
})

const linksRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/links',
  component: LinkPage,
})

const pagesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/pages',
  component: PagesPage,
})

const trashRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/trash',
  component: TrashPage,
})

const logsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/logs',
  component: LogPage,
})

const importRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/import',
  component: ImportPage,
})

const backupsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/backups',
  component: BackupPage,
})

const activityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/activity',
  component: SystemActivityPage,
})

const routeTree = rootRoute.addChildren([
  indexRoute,
  postsRoute,
  postEditRoute,
  mediaRoute,
  translationsRoute,
  themesRoute,
  settingsRoute,
  usersRoute,
  categoriesRoute,
  tagsRoute,
  menusRoute,
  linksRoute,
  pagesRoute,
  trashRoute,
  logsRoute,
  importRoute,
  backupsRoute,
  activityRoute,
])

export const router = createRouter({ routeTree })
