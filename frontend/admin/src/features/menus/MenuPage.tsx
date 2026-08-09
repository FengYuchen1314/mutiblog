// 菜单管理：编辑菜单项（自定义 + 从文章/页面添加），保存到后端。
import { useEffect, useState } from 'react'
import { api, csrf, type API } from '../../api/client'

type Menu = API['Menu']
type MenuItem = API['MenuItem']

const LOCALES = ['zh-CN', 'en']

type Target = { id: string; title: string; slug: string; type: string }

function blankItem(): MenuItem {
  return {
    id: '',
    type: 'custom',
    ref: '',
    url: '',
    icon: '',
    target: '',
    label: {},
    order: 0,
    children: [],
  }
}

export default function MenuPage() {
  const [menus, setMenus] = useState<Menu[]>([])
  const [selected, setSelected] = useState<Menu | null>(null)
  const [targets, setTargets] = useState<Target[]>([])
  const [targetType, setTargetType] = useState('post')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const refresh = () =>
    api('/api/admin/menus/')
      .then((data) => {
        setMenus(data.items || [])
        if (!selected && (data.items || []).length > 0) setSelected(data.items[0])
      })
      .catch((e) => setError(e.message))
  useEffect(() => {
    refresh()
  }, [])
  const loadTargets = async () => {
    try {
      const data = await api('/api/admin/menus/targets?type=' + targetType + '&locale=zh-CN')
      setTargets(data.items || [])
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取可添加项失败')
    }
  }
  useEffect(() => {
    void loadTargets()
  }, [targetType])
  const save = async () => {
    if (!selected) return
    try {
      const token = await csrf()
      await api('/api/admin/menus/' + encodeURIComponent(selected.id), {
        method: 'PUT',
        headers: { 'X-CSRF-Token': token },
        body: JSON.stringify(selected),
      })
      setNotice('菜单已保存，站点将刷新。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存菜单失败')
    }
  }
  const addTarget = (target: Target) => {
    if (!selected) return
    const prefix = targetType === 'page' ? '' : 'posts/'
    const item: MenuItem = {
      ...blankItem(),
      id: target.id,
      type: target.type,
      ref: target.slug,
      url: '/zh-cn/' + prefix + target.slug + '/',
      label: { 'zh-CN': target.title },
    }
    setSelected({ ...selected, items: [...selected.items, item] })
  }
  const setItem = (index: number, patch: Partial<MenuItem>) => {
    if (!selected) return
    const items = selected.items.map((item, i) => (i === index ? { ...item, ...patch } : item))
    setSelected({ ...selected, items })
  }
  const setChild = (index: number, childIndex: number, patch: Partial<MenuItem>) => {
    if (!selected) return
    const items = selected.items.map((item, i) => {
      if (i !== index) return item
      const children = (item.children || []).map((child, j) =>
        j === childIndex ? { ...child, ...patch } : child,
      )
      return { ...item, children }
    })
    setSelected({ ...selected, items })
  }
  return (
    <section className="menus">
      <div className="activity-heading">
        <h2>菜单</h2>
        <button type="button" onClick={() => void save()}>
          保存菜单
        </button>
      </div>
      <select
        value={selected?.id || ''}
        onChange={(e) => setSelected(menus.find((menu) => menu.id === e.target.value) || null)}
      >
        {menus.map((menu) => (
          <option key={menu.id} value={menu.id}>
            {menu.name?.['zh-CN'] || menu.id}
          </option>
        ))}
      </select>
      {selected && (
        <form
          className="menu-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <label>
            ID
            <input
              value={selected.id}
              disabled
              onChange={(e) => setSelected({ ...selected, id: e.target.value })}
            />
          </label>
          {LOCALES.map((locale) => (
            <label key={locale}>
              名称（{locale}）
              <input
                value={selected.name?.[locale] || ''}
                onChange={(e) =>
                  setSelected({
                    ...selected,
                    name: { ...(selected.name || {}), [locale]: e.target.value },
                  })
                }
              />
            </label>
          ))}
          <div className="menu-targets">
            <select
              value={targetType}
              onChange={(e) => setTargetType(e.target.value)}
              aria-label="可添加项类型"
            >
              <option value="post">文章</option>
              <option value="page">页面</option>
            </select>
            <select
              defaultValue=""
              aria-label="可添加项"
              onChange={(e) => {
                const target = targets.find((item) => item.id === e.target.value)
                if (target) addTarget(target)
              }}
            >
              <option value="">添加…</option>
              {targets.map((target) => (
                <option key={target.id} value={target.id}>
                  {target.title}
                </option>
              ))}
            </select>
            <button
              type="button"
              className="secondary"
              onClick={() => setSelected({ ...selected, items: [...selected.items, blankItem()] })}
            >
              添加自定义项
            </button>
          </div>
          <div className="menu-items">
            {selected.items.map((item, index) => (
              <fieldset key={index} className="menu-item">
                <legend>菜单项 {index + 1}</legend>
                <div className="menu-item-row">
                  <label>
                    标签
                    <input
                      value={item.label?.['zh-CN'] || ''}
                      onChange={(e) =>
                        setItem(index, {
                          label: { ...(item.label || {}), 'zh-CN': e.target.value },
                        })
                      }
                    />
                  </label>
                  <label>
                    链接
                    <input
                      value={item.url || ''}
                      onChange={(e) => setItem(index, { url: e.target.value })}
                    />
                  </label>
                  <button
                    type="button"
                    className="danger"
                    onClick={() =>
                      setSelected({
                        ...selected,
                        items: selected.items.filter((_, i) => i !== index),
                      })
                    }
                  >
                    删除
                  </button>
                </div>
                {(item.children || []).map((child, childIndex) => (
                  <div className="menu-item-row menu-child" key={childIndex}>
                    <label>
                      子项标签
                      <input
                        value={child.label?.['zh-CN'] || ''}
                        onChange={(e) =>
                          setChild(index, childIndex, {
                            label: { ...(child.label || {}), 'zh-CN': e.target.value },
                          })
                        }
                      />
                    </label>
                    <label>
                      子项链接
                      <input
                        value={child.url || ''}
                        onChange={(e) => setChild(index, childIndex, { url: e.target.value })}
                      />
                    </label>
                    <button
                      type="button"
                      className="danger"
                      onClick={() =>
                        setItem(index, {
                          children: (item.children || []).filter((_, j) => j !== childIndex),
                        })
                      }
                    >
                      删除
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  className="secondary"
                  onClick={() =>
                    setItem(index, { children: [...(item.children || []), blankItem()] })
                  }
                >
                  添加子项
                </button>
              </fieldset>
            ))}
            {selected.items.length === 0 && <p>暂无菜单项。</p>}
          </div>
        </form>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
