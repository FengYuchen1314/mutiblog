// 站点页脚：版权与年份。
export default function Footer() {
  return (
    <footer className="site-footer">
      <p>© {new Date().getUTCFullYear()} Mutiblog</p>
    </footer>
  )
}
