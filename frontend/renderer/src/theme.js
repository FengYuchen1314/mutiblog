// 主题 SSR bundle 加载与缓存，以及客户端资源清单读取。
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { renderToStaticMarkup } from 'react-dom/server'
import { createElement } from 'react'

const themeCache = new Map()

/**
 * 渲染主题模板为 body 内容。
 * 主题不可用或渲染失败时返回空字符串，由调用方降级。
 */
export async function renderTheme(props, html) {
  const entry = props.themeDir && path.join(props.themeDir, 'dist', 'ssr', 'entry.js')
  if (!entry || !fs.existsSync(entry)) return ''
  try {
    const stamp = fs.statSync(entry).mtimeMs
    let mod = themeCache.get(entry)
    if (!mod || mod.stamp !== stamp) {
      mod = { stamp, value: await import(pathToFileURL(entry).href + '?v=' + stamp) }
      themeCache.set(entry, mod)
    }
    const template = mod.value.templates?.[props.kind]
    if (template) {
      return renderToStaticMarkup(createElement(template, { ...props, html }))
    }
    if (typeof mod.value.render === 'function') return await mod.value.render({ ...props, html })
    return ''
  } catch (err) {
    console.error('theme SSR fallback:', err)
    return ''
  }
}

/** 读取主题 manifest，返回注入 head 的脚本与样式片段。 */
export async function themeAssetUrls(themeDir) {
  if (!themeDir) return { scripts: '', styles: '' }
  try {
    const manifestPath = path.join(themeDir, 'dist', 'manifest.json')
    if (!fs.existsSync(manifestPath)) return { scripts: '', styles: '' }
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
    const scripts = []
    const styles = []
    for (const url of manifest.client || []) {
      scripts.push(
        '<script type="module" crossorigin src="/assets/' + path.basename(url) + '"></script>',
      )
    }
    for (const url of manifest.css || []) {
      styles.push('<link rel="stylesheet" href="/assets/' + path.basename(url) + '">')
    }
    return { scripts: scripts.join(''), styles: styles.join('') }
  } catch (err) {
    console.warn('theme assets:', err)
    return { scripts: '', styles: '' }
  }
}
