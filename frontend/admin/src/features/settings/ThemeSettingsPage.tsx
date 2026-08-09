// 主题设置：主题列表、schema 驱动的表单、激活与恢复默认。
import { useEffect, useState } from 'react'
import { api, csrf } from '../../api/client'
import type { ThemeField, ThemeItem } from '../../api/types'

function ThemeArrayField({
  field,
  value,
  onChange,
}: {
  field: ThemeField & { items?: ThemeField[] }
  value: unknown
  onChange: (value: Record<string, unknown>[]) => void
}) {
  const rows = Array.isArray(value)
    ? (value.filter((item) => item && typeof item === 'object') as Record<string, unknown>[])
    : []
  const title = (item: ThemeField) => item.label?.['zh-CN'] || item.label?.en || item.key
  const edit = (row: number, key: string, next: unknown) =>
    onChange(rows.map((item, index) => (index === row ? { ...item, [key]: next } : item)))
  return (
    <fieldset className="theme-array">
      <legend>{title(field)}</legend>
      {rows.map((item, row) => (
        <div className="theme-array-item" key={row}>
          {(field.items || []).map((child) =>
            child.type === 'select' ? (
              <label key={child.key}>
                {title(child)}
                <select
                  value={String(item[child.key] ?? child.default ?? '')}
                  onChange={(e) => edit(row, child.key, e.target.value)}
                >
                  {(child.options || []).map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label?.['zh-CN'] || option.label?.en || option.value}
                    </option>
                  ))}
                </select>
              </label>
            ) : (
              <label key={child.key}>
                {title(child)}
                <input
                  value={String(item[child.key] ?? child.default ?? '')}
                  onChange={(e) => edit(row, child.key, e.target.value)}
                />
              </label>
            ),
          )}
          <button
            type="button"
            className="danger"
            onClick={() => onChange(rows.filter((_, index) => index !== row))}
          >
            删除
          </button>
        </div>
      ))}
      <button
        type="button"
        className="secondary"
        onClick={() =>
          onChange([
            ...rows,
            Object.fromEntries(
              (field.items || []).map((child) => [child.key, child.default ?? '']),
            ),
          ])
        }
      >
        添加一项
      </button>
    </fieldset>
  )
}

function ThemeI18nField({
  field,
  value,
  onChange,
}: {
  field: ThemeField
  value: unknown
  onChange: (value: Record<string, string>) => void
}) {
  const values =
    value && typeof value === 'object' && !Array.isArray(value)
      ? (value as Record<string, string>)
      : {}
  const locales = [...new Set(['zh-CN', 'en', ...Object.keys(values)])]
  const label = (locale: string) =>
    locale === 'zh-CN' ? '简体中文' : locale === 'en' ? 'English' : locale
  const textarea = field.type === 'i18n-textarea'
  return (
    <fieldset className="theme-i18n">
      <legend>{field.label?.['zh-CN'] || field.label?.en || field.key}</legend>
      {locales.map((locale) => (
        <label key={locale}>
          {label(locale)}
          {textarea ? (
            <textarea
              rows={field.rows || 3}
              value={values[locale] || ''}
              onChange={(e) => onChange({ ...values, [locale]: e.target.value })}
            />
          ) : (
            <input
              value={values[locale] || ''}
              onChange={(e) => onChange({ ...values, [locale]: e.target.value })}
            />
          )}
        </label>
      ))}
    </fieldset>
  )
}

export default function ThemeSettingsPage() {
  const [items, setItems] = useState<ThemeItem[]>([]),
    [active, setActive] = useState(''),
    [selected, setSelected] = useState<ThemeItem | null>(null),
    [values, setValues] = useState<Record<string, unknown>>({}),
    [notice, setNotice] = useState(''),
    [error, setError] = useState('')
  const select = async (item: ThemeItem) => {
    try {
      const detail = await api('/api/admin/themes/' + encodeURIComponent(item.manifest.name))
      setSelected(detail.theme)
      setValues(detail.values || {})
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '读取主题失败')
    }
  }
  const refresh = () =>
    api('/api/admin/themes/')
      .then((data) => {
        setItems(data.items || [])
        setActive(data.active || '')
        const current = (data.items || []).find(
          (item: ThemeItem) => item.manifest.name === (selected?.manifest.name || data.active),
        )
        if (current) void select(current)
      })
      .catch((e) => setError(e instanceof Error ? e.message : '读取主题列表失败'))
  useEffect(() => {
    refresh()
  }, [])
  const activate = async (name: string) => {
    if (!window.confirm('切换主题会重新生成整个静态站点。继续吗？')) return
    try {
      const token = await csrf()
      await api('/api/admin/themes/' + encodeURIComponent(name) + '/activate', {
        method: 'POST',
        headers: { 'X-CSRF-Token': token },
      })
      setActive(name)
      setNotice('主题已切换，正在重新生成站点。')
      refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : '切换主题失败')
    }
  }
  const save = async (reset = false) => {
    if (!selected) return
    try {
      const token = await csrf()
      const suffix = reset ? '/settings/reset' : '/settings'
      const result = await api(
        '/api/admin/themes/' + encodeURIComponent(selected.manifest.name) + suffix,
        {
          method: reset ? 'POST' : 'PUT',
          headers: { 'X-CSRF-Token': token },
          body: reset ? undefined : JSON.stringify(values),
        },
      )
      setValues(result.values || {})
      setNotice(reset ? '已恢复主题默认设置。' : '主题设置已保存，正在重新生成站点。')
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : '保存主题设置失败')
    }
  }
  const fields = (selected?.schema?.fields || []).filter(
    (field) =>
      field.type !== 'group-divider' &&
      (!field.showIf || values[field.showIf.key] === field.showIf.equals),
  )
  const label = (field: ThemeField) => field.label?.['zh-CN'] || field.label?.en || field.key
  const help = (field: ThemeField) => field.help?.['zh-CN'] || field.help?.en
  return (
    <section className="themes">
      <h2>主题</h2>
      <div className="theme-picker">
        {items.map((item) => (
          <button
            key={item.manifest.name}
            className={selected?.manifest.name === item.manifest.name ? 'selected' : ''}
            type="button"
            onClick={() => void select(item)}
          >
            {item.manifest.displayName?.['zh-CN'] ||
              item.manifest.displayName?.en ||
              item.manifest.name}
            {active === item.manifest.name && <small>当前</small>}
          </button>
        ))}
      </div>
      {selected && (
        <form
          className="theme-form"
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <div className="theme-heading">
            <h3>{selected.manifest.displayName?.['zh-CN'] || selected.manifest.name}</h3>
            {active !== selected.manifest.name && (
              <button type="button" onClick={() => void activate(selected.manifest.name)}>
                激活主题
              </button>
            )}
          </div>
          {fields.map((field) => {
            const value = values[field.key] ?? field.default ?? ''
            const meta = help(field) && <small>{help(field)}</small>
            if (field.type === 'array')
              return (
                <div key={field.key}>
                  <ThemeArrayField
                    field={field}
                    value={value}
                    onChange={(next) => setValues({ ...values, [field.key]: next })}
                  />
                  {meta}
                </div>
              )
            if (field.type === 'i18n-text' || field.type === 'i18n-textarea')
              return (
                <div key={field.key}>
                  <ThemeI18nField
                    field={field}
                    value={value}
                    onChange={(next) => setValues({ ...values, [field.key]: next })}
                  />
                  {meta}
                </div>
              )
            if (field.type === 'boolean')
              return (
                <label className="check" key={field.key}>
                  <input
                    type="checkbox"
                    checked={Boolean(value)}
                    onChange={(e) => setValues({ ...values, [field.key]: e.target.checked })}
                  />
                  {label(field)}
                  {meta}
                </label>
              )
            if (field.type === 'radio')
              return (
                <fieldset key={field.key}>
                  <legend>{label(field)}</legend>
                  {(field.options || []).map((option) => (
                    <label className="radio" key={option.value}>
                      <input
                        type="radio"
                        name={field.key}
                        checked={value === option.value}
                        onChange={() => setValues({ ...values, [field.key]: option.value })}
                      />
                      {option.label?.['zh-CN'] || option.label?.en || option.value}
                    </label>
                  ))}
                  {meta}
                </fieldset>
              )
            if (field.type === 'select' || field.type === 'multiselect')
              return (
                <label key={field.key}>
                  {label(field)}
                  <select
                    multiple={field.type === 'multiselect'}
                    value={
                      field.type === 'multiselect'
                        ? Array.isArray(value)
                          ? value.map(String)
                          : []
                        : String(value)
                    }
                    onChange={(e) =>
                      setValues({
                        ...values,
                        [field.key]:
                          field.type === 'multiselect'
                            ? Array.from(e.currentTarget.selectedOptions, (option) => option.value)
                            : e.target.value,
                      })
                    }
                  >
                    {(field.options || []).map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label?.['zh-CN'] || option.label?.en || option.value}
                      </option>
                    ))}
                  </select>
                  {meta}
                </label>
              )
            if (field.type === 'textarea' || field.type === 'code')
              return (
                <label key={field.key}>
                  {label(field)}
                  <textarea
                    rows={field.rows || undefined}
                    maxLength={field.maxLength}
                    placeholder={field.placeholder}
                    required={field.required}
                    value={String(value)}
                    onChange={(e) => setValues({ ...values, [field.key]: e.target.value })}
                  />
                  {meta}
                </label>
              )
            const inputType =
              field.type === 'number'
                ? 'number'
                : field.type === 'color'
                  ? 'color'
                  : field.type === 'url'
                    ? 'url'
                    : 'text'
            return (
              <label key={field.key}>
                {label(field)}
                <input
                  type={inputType}
                  min={field.min}
                  max={field.max}
                  step={field.step}
                  maxLength={field.maxLength}
                  pattern={field.pattern}
                  placeholder={field.placeholder}
                  required={field.required}
                  value={String(value)}
                  onChange={(e) =>
                    setValues({
                      ...values,
                      [field.key]:
                        field.type === 'number' ? Number(e.target.value) : e.target.value,
                    })
                  }
                />
                {meta}
              </label>
            )
          })}
          <div className="theme-actions">
            <button>保存主题设置</button>
            <button type="button" className="secondary" onClick={() => void save(true)}>
              恢复默认
            </button>
          </div>
        </form>
      )}
      {notice && <p className="notice">{notice}</p>}
      {error && <p className="error">{error}</p>}
    </section>
  )
}
