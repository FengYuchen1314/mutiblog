// 正文容器：注入渲染器输出的 Markdown HTML。
export default function Prose({ html }: { html: string }) {
  return <div className="prose" dangerouslySetInnerHTML={{ __html: html }} />
}
