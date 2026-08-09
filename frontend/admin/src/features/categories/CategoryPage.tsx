// 分类管理：可拖拽排序的树状列表、编辑表单、父级调整与删除迁移。
import { useEffect, useMemo, useState } from 'react'
import {
  closestCenter,
  DndContext,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  arrayMove,
  SortableContext,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { api, csrf, type API } from '../../api/client'

type Category = API['Category']

const LOCALES = ['zh-CN', 'en']

function depthOf(items: Category[], id: string, seen: Set<string>): number {
  if (seen.has(id)) return 0
  const item = items.find((entry) => entry.id === id)
  if (!item || !item.parent || item.parent === id) return 0
  seen.add(id)
  return 1 + depthOf(items, item.parent, seen)
}

function SortableRow({
  category,
  depth,
  selected,
  onEdit,
  onDelete,
}: {
  category: Category
  depth: number
  selected: boolean
  onEdit: () => void
  onDelete: () => void
}) {
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({
    id: category.id,
  })
  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  }
  const label = category.name?.['zh-CN'] || category.slug || category.id
  return (
    <div ref={setNodeRef} style={style} className={'category-row' + (selected ? ' selected' : '')}>
      <button
        type="button"
        className="posttitle category-label"
        style={{ paddingLeft: depth * 1.4 + 'rem' }}
        {...attributes}
        {...listeners}
      >
        {label}
      </button>
      <div className="category-actions">
        <button type="button" className="secondary" onClick={onEdit}>
          编辑
        </button>
        <button type="button" className="danger" onClick={onDelete}>
          删除
        </button>
      </div>
    </div>
  )
}

export default function CategoryPage() {
  const [items, setItems] = useState<Category[]>([])
  const [editing, setEditing] = useState<Category | null>(null)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }))
  const refresh = () =>
    api('/api/admin/categories/')
      .then((data) => setItems(data.items || []))
      .catch((e) => setError(e.message))
  useEffect(() => {
    refresh()
  }, [])
  const depths = useMemo(() => {
    const map: Record<string, number> = {}
    for (const item of items) map[item.id] = depthOf(items, item.id, new Set())
    return map
  }, [items])
  const save = async () => {
    if (!editing) return
    try {
      const token = await csrf()
      const method = editing.id ? 'PUT' : 'POST'
      const path = editing.id
        ? '/api/admin/categories/' + encodeURIComponent(editing.id)
        : '/api/admin/categories/'
      await api(path, {
        method,
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(editing),
      })
      setNotice('分类已保存，站点将刷新。')
      setEditing(null)
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存分类失败')
    }
  }
  const remove = async (category: Category) => {
    const message = window.confirm(
      '删除分类 ' +
        (category.name?.['zh-CN'] || category.id) +
        '？\n若被文章引用，请输入迁移目标分类 ID（可留空）。',
    )
    if (!message) return
    const target = window.prompt('迁移目标分类 ID（留空则不迁移）：') || ''
    try {
      const token = await csrf()
      const query = target ? '?migrateTo=' + encodeURIComponent(target) : ''
      await api('/api/admin/categories/' + encodeURIComponent(category.id) + query, {
        method: 'DELETE',
        headers: { 'X-CSRF-Token': token },
      })
      setNotice('分类已删除。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '删除分类失败')
    }
  }
  const onDragEnd = async (event: DragEndEvent) => {
    if (!event.over || event.active.id === event.over.id) return
    const from = items.findIndex((item) => item.id === event.active.id)
    const to = items.findIndex((item) => item.id === event.over?.id)
    const next = arrayMove(items, from, to)
    const reordered = next.map((item, index) => ({ ...item, order: index + 1 }))
    setItems(reordered)
    try {
      const token = await csrf()
      for (const item of reordered) {
        await api('/api/admin/categories/' + encodeURIComponent(item.id), {
          method: 'PUT',
          headers: { 'X-CSRF-Token': token },
          body: JSON.stringify(item),
        })
      }
      setNotice('排序已保存。')
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存排序失败')
      refresh()
    }
  }
  const setField = (key: keyof Category, value: unknown) => {
    setEditing((current) => (current ? { ...current, [key]: value } : current))
  }
  const setLocalized = (key: 'name' | 'description', locale: string, value: string) => {
    setEditing((current) => {
      if (!current) return current
      const map = { ...(current[key] || {}) }
      map[locale] = value
      return { ...current, [key]: map }
    })
  }
  return (
    <section className="categories">
      <div className="activity-heading">
        <h2>分类</h2>
        <button type="button" onClick={() => setEditing({ id: '', name: {}, description: {} })}>
          新建分类
        </button>
      </div>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
        <SortableContext
          items={items.map((item) => item.id)}
          strategy={verticalListSortingStrategy}
        >
          <div className="category-list">
            {items.map((item) => (
              <SortableRow
                key={item.id}
                category={item}
                depth={depths[item.id] || 0}
                selected={editing?.id === item.id}
                onEdit={() => setEditing({ ...item })}
                onDelete={() => void remove(item)}
              />
            ))}
          </div>
        </SortableContext>
      </DndContext>
      {items.length === 0 && <p>暂无分类。</p>}
      {editing && (
        <form
          className="category-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <h3>{editing.id ? '编辑分类' : '新建分类'}</h3>
          <label>
            ID
            <input
              required
              value={editing.id}
              disabled={Boolean(editing.id)}
              onChange={(e) => setField('id', e.target.value)}
            />
          </label>
          <label>
            Slug
            <input value={editing.slug || ''} onChange={(e) => setField('slug', e.target.value)} />
          </label>
          <label>
            父分类
            <select
              value={editing.parent || ''}
              onChange={(e) => setField('parent', e.target.value || undefined)}
            >
              <option value="">（无）</option>
              {items
                .filter((item) => item.id !== editing.id)
                .map((item) => (
                  <option key={item.id} value={item.id}>
                    {item.name?.['zh-CN'] || item.slug || item.id}
                  </option>
                ))}
            </select>
          </label>
          <label>
            排序
            <input
              type="number"
              value={editing.order || 0}
              onChange={(e) => setField('order', Number(e.target.value))}
            />
          </label>
          {LOCALES.map((locale) => (
            <div className="category-locale" key={locale}>
              <h4>{locale}</h4>
              <label>
                名称
                <input
                  value={editing.name?.[locale] || ''}
                  onChange={(e) => setLocalized('name', locale, e.target.value)}
                />
              </label>
              <label>
                描述
                <textarea
                  rows={2}
                  value={editing.description?.[locale] || ''}
                  onChange={(e) => setLocalized('description', locale, e.target.value)}
                />
              </label>
            </div>
          ))}
          <div className="theme-actions">
            <button>保存</button>
            <button type="button" className="secondary" onClick={() => setEditing(null)}>
              取消
            </button>
          </div>
        </form>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
