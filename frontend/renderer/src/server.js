// HTTP 服务入口：Unix Socket 路由分发到 render / markdown。
import http from 'node:http'
import fs from 'node:fs'
import { render } from './render.js'
import { renderMarkdown } from './markdown.js'

const socket = process.env.BLOG_RENDER_SOCKET || '/tmp/blog-render.sock'
try {
  fs.unlinkSync(socket)
} catch (e) {
  if (e.code !== 'ENOENT') throw e
}

const server = http.createServer((req, res) => {
  if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200, { 'content-type': 'application/json' })
    res.end('{"ok":true}')
    return
  }
  if (req.method === 'POST' && req.url === '/reload') {
    res.writeHead(200, { 'content-type': 'application/json' })
    res.end('{"ok":true}')
    return
  }
  if (req.method === 'POST' && (req.url === '/render' || req.url === '/markdown')) {
    let data = ''
    req.on('data', (chunk) => {
      data += chunk
      if (data.length > 6 * 1024 * 1024) {
        req.destroy()
      }
    })
    req.on('end', async () => {
      try {
        const props = JSON.parse(data)
        const source =
          props.body ??
          props.markdownText ??
          (typeof props.markdown === 'string' ? props.markdown : '')
        const options =
          props.markdownOptions || (typeof props.markdown === 'object' ? props.markdown : {})
        const html =
          req.url === '/markdown'
            ? (await renderMarkdown(source, options)).html
            : await render(props)
        res.writeHead(200, { 'content-type': 'application/json' })
        res.end(JSON.stringify({ html }))
      } catch (err) {
        res.writeHead(400, { 'content-type': 'application/json' })
        res.end(JSON.stringify({ error: String(err) }))
      }
    })
    return
  }
  res.writeHead(404)
  res.end()
})

server.listen(socket, () => {
  try {
    fs.chmodSync(socket, 0o600)
  } catch {
    // 权限设置失败不影响监听
  }
})

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => server.close(() => process.exit(0)))
}
