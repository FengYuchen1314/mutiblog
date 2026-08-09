// CodeMirror Markdown 编辑器：受控 value + onChange，文档变更时回调。
import { useEffect, useRef } from 'react'
import { EditorState } from '@codemirror/state'
import { EditorView, keymap } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { markdown } from '@codemirror/lang-markdown'
import { oneDark } from '@codemirror/theme-one-dark'

export default function MarkdownEditor({
  value,
  onChange,
}: {
  value: string
  onChange: (value: string) => void
}) {
  const host = useRef<HTMLDivElement>(null),
    view = useRef<EditorView | null>(null),
    latest = useRef(value)
  useEffect(() => {
    latest.current = value
  }, [value])
  useEffect(() => {
    if (!host.current) return
    view.current = new EditorView({
      state: EditorState.create({
        doc: value,
        extensions: [
          history(),
          keymap.of([...defaultKeymap, ...historyKeymap]),
          markdown(),
          oneDark,
          EditorView.lineWrapping,
          EditorView.updateListener.of((update) => {
            if (update.docChanged) onChange(update.state.doc.toString())
          }),
        ],
      }),
      parent: host.current,
    })
    return () => {
      view.current?.destroy()
      view.current = null
    }
  }, [])
  useEffect(() => {
    const current = view.current?.state.doc.toString()
    if (view.current && current !== value && value === latest.current)
      view.current.dispatch({ changes: { from: 0, to: current.length, insert: value } })
  }, [value])
  return <div className="editor-body codemirror" ref={host} />
}
