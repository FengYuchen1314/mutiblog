// SSR 侧的 Island 占位：输出 data-island 标记与无 JS 时的降级内容。
// T3 将实现两遍渲染（收集 island → 逐岛 renderToString → 替换）。
export default function Island({
  name,
  props,
  hydrate = 'idle',
  children,
}: {
  name: string
  props?: Record<string, unknown>
  hydrate?: 'load' | 'idle' | 'visible'
  children: React.ReactNode
}) {
  return (
    <div
      data-island={name}
      data-island-hydrate={hydrate}
      data-island-props={JSON.stringify(props || {})}
    >
      {children}
    </div>
  )
}
