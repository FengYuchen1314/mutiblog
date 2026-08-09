// 后台布局：侧边栏导航 + 内容出口（路由页面渲染于此）。
import { Link, Outlet } from '@tanstack/react-router'

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
        <Link to="/import">导入</Link>
        <Link to="/backups">备份</Link>
        <Link to="/activity">活动</Link>
      </aside>
      <section>
        <Outlet />
      </section>
    </main>
  )
}
