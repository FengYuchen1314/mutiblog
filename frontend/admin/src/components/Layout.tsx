// 后台布局：侧边栏导航 + 内容出口（路由页面渲染于此）。
import { Link, Outlet } from '@tanstack/react-router'
import ActivityIndicator from './ActivityIndicator'

export default function Layout() {
  return (
    <main className="dashboard">
      <aside>
        <strong>Mutiblog</strong>
        <Link to="/">仪表盘</Link>
        <Link to="/posts">文章</Link>
        <Link to="/media">媒体</Link>
        <Link to="/translations">翻译任务</Link>
        <Link to="/themes">主题</Link>
        <Link to="/settings">设置</Link>
        <Link to="/users">用户</Link>
        <Link to="/categories">分类</Link>
        <Link to="/tags">标签</Link>
        <Link to="/menus">菜单</Link>
        <Link to="/links">友链</Link>
        <Link to="/pages">页面</Link>
        <Link to="/trash">回收站</Link>
        <Link to="/logs">日志</Link>
        <Link to="/import">导入</Link>
        <Link to="/backups">备份</Link>
        <Link to="/activity">活动</Link>
        <ActivityIndicator />
      </aside>
      <section>
        <Outlet />
      </section>
    </main>
  )
}
