// 从 Vite 的 .vite/manifest.json 归一化产出主题 manifest.json。
import { readFileSync, writeFileSync, existsSync, mkdirSync } from 'node:fs'
import { resolve, dirname } from 'node:path'

const root = resolve(import.meta.dirname, '..')
const source = resolve(root, 'dist', '.vite', 'manifest.json')
const target = resolve(root, 'dist', 'manifest.json')

if (!existsSync(source)) {
  console.error('missing vite manifest at', source)
  process.exit(1)
}

const viteManifest = JSON.parse(readFileSync(source, 'utf8'))
const files = Object.values(viteManifest)
const client = []
const css = []

for (const file of files) {
  if (file.file.endsWith('.js')) client.push('/' + file.file)
  if (file.file.endsWith('.css')) css.push('/' + file.file)
  for (const item of file.assets || []) {
    if (item.endsWith('.js')) client.push('/' + item)
    if (item.endsWith('.css')) css.push('/' + item)
  }
  for (const item of file.css || []) {
    css.push('/' + item)
  }
}

const manifest = {
  client: [...new Set(client)],
  css: [...new Set(css)],
  chunks: {},
  static: [],
}

mkdirSync(dirname(target), { recursive: true })
writeFileSync(target, JSON.stringify(manifest, null, 2) + '\n')
console.log('theme manifest written:', target)
