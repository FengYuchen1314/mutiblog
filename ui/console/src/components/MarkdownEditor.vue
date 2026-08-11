<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { basicSetup } from "codemirror";
import { markdown } from "@codemirror/lang-markdown";
import { EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { createPreviewMarkdownRenderer } from "./markdownPreview";
import { typesetPreviewMath } from "./mathJaxPreview";

const props = defineProps<{ modelValue: string; uploading?: boolean; sourceComparison?: { locale: string; title: string; markdown: string } }>();
const { t } = useI18n();
const emit = defineEmits<{
  (event: "update:modelValue", value: string): void;
  (event: "image-files", files: File[]): void;
}>();

const host = ref<HTMLElement>();
const container = ref<HTMLElement>();
const previewElement = ref<HTMLElement>();
const mode = ref<"edit" | "split" | "preview">("split");
const fullscreen = ref(false);
const comparisonOpen = ref(false);
let editor: EditorView | undefined;
let syncingExternalValue = false;
let syncingScroll = false;

// The shared renderer embeds MathJax's server-side SVG pipeline. The editor
// instead uses a browser-only self-hosted component after Vue updates its own
// preview DOM, keeping the console bootstrap free of server-only modules.
const previewRenderer = createPreviewMarkdownRenderer();
const preview = computed(() => previewRenderer.render(props.modelValue));
let previewMathTimer: ReturnType<typeof setTimeout> | undefined;

onMounted(() => {
  editor = new EditorView({
    parent: host.value,
    state: EditorState.create({
      doc: props.modelValue,
      extensions: [
        basicSetup,
        markdown(),
        EditorView.lineWrapping,
		keymap.of([
		  { key: "Mod-b", run: (view) => wrapSelection(view, "**", "**", t("markdownEditor.boldPlaceholder")) },
		  { key: "Mod-i", run: (view) => wrapSelection(view, "_", "_", t("markdownEditor.italicPlaceholder")) },
		  { key: "Mod-k", run: (view) => insertLink(view) },
		]),
        EditorView.updateListener.of((update) => {
          if (update.docChanged && !syncingExternalValue) emit("update:modelValue", update.state.doc.toString());
        }),
        EditorView.domEventHandlers({
          paste(event) {
            const files = [...(event.clipboardData?.files ?? [])].filter((file) => file.type.startsWith("image/"));
            if (files.length === 0) return false;
            event.preventDefault();
            emit("image-files", files);
            return true;
          },
          drop(event) {
            const files = [...(event.dataTransfer?.files ?? [])].filter((file) => file.type.startsWith("image/"));
            if (files.length === 0) return false;
            event.preventDefault();
            emit("image-files", files);
            return true;
          },
        }),
      ],
    }),
  });
  editor.scrollDOM.addEventListener("scroll", handleEditorScroll, { passive: true });
  schedulePreviewMath();
});

watch(
  () => props.modelValue,
  (value) => {
    if (!editor || editor.state.doc.toString() === value) return;
    syncingExternalValue = true;
    editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } });
    syncingExternalValue = false;
  },
);

watch(preview, () => schedulePreviewMath(), { flush: "post" });

onBeforeUnmount(() => {
  if (previewMathTimer) clearTimeout(previewMathTimer);
  editor?.scrollDOM.removeEventListener("scroll", handleEditorScroll);
  editor?.destroy();
});

function schedulePreviewMath() {
  if (previewMathTimer) clearTimeout(previewMathTimer);
  previewMathTimer = setTimeout(() => {
    const target = previewElement.value;
    if (!target) return;
    void typesetPreviewMath(target).catch(() => {
      // A formula error is represented by MathJax inside the preview. A failed
      // lazy asset load must not destabilise the editor itself.
    });
  }, 250);
}

function handleEditorScroll() { if (editor) synchronizeScroll(editor.scrollDOM); }
function handlePaneScroll(event: Event) { synchronizeScroll(event.currentTarget as HTMLElement); }
function synchronizeScroll(source: HTMLElement) {
  if (syncingScroll) return;
  const maximum = source.scrollHeight - source.clientHeight;
  if (maximum <= 0) return;
  syncingScroll = true;
  const ratio = source.scrollTop / maximum;
  const targets = [editor?.scrollDOM, ...Array.from(container.value?.querySelectorAll<HTMLElement>(".source-comparison,.markdown-preview") ?? [])];
  for (const target of targets) {
    if (target && target !== source && target.offsetParent !== null) target.scrollTop = ratio * Math.max(0, target.scrollHeight - target.clientHeight);
  }
  requestAnimationFrame(() => { syncingScroll = false; });
}

function insertAtCursor(text: string) {
  if (!editor) return;
  const selection = editor.state.selection.main;
  editor.dispatch({ changes: { from: selection.from, to: selection.to, insert: text }, selection: { anchor: selection.from + text.length } });
  editor.focus();
}

function wrapSelection(view: EditorView, before: string, after: string, placeholder: string) {
  const selection = view.state.selection.main;
  const selected = view.state.sliceDoc(selection.from, selection.to) || placeholder;
  view.dispatch({
    changes: { from: selection.from, to: selection.to, insert: before + selected + after },
    selection: { anchor: selection.from + before.length, head: selection.from + before.length + selected.length },
  });
  view.focus();
  return true;
}

function prefixSelection(prefix: string) {
  if (!editor) return;
  const selection = editor.state.selection.main;
  const selected = editor.state.sliceDoc(selection.from, selection.to) || t("markdownEditor.textPlaceholder");
  const replacement = selected.split("\n").map((line) => prefix + line).join("\n");
  editor.dispatch({ changes: { from: selection.from, to: selection.to, insert: replacement }, selection: { anchor: selection.from, head: selection.from + replacement.length } });
  editor.focus();
}

function insertLink(view = editor) {
  if (!view) return false;
  const selection = view.state.selection.main;
  const selected = view.state.sliceDoc(selection.from, selection.to) || t("markdownEditor.linkPlaceholder");
  const replacement = `[${selected}](https://)`;
  view.dispatch({ changes: { from: selection.from, to: selection.to, insert: replacement }, selection: { anchor: selection.from + selected.length + 3 } });
  view.focus();
  return true;
}

function showPreview() {
  mode.value = "preview";
  container.value?.scrollIntoView({ behavior: "smooth", block: "start" });
}

defineExpose({ insertAtCursor, showPreview });
</script>

<template>
  <div ref="container" class="markdown-editor" :class="{ 'markdown-editor--fullscreen': fullscreen }">
    <div class="editor-toolbar">
      <div class="mode-tabs">
        <button :class="{ active: mode === 'edit' }" type="button" @click="mode = 'edit'">{{ t("markdownEditor.markdown") }}</button>
        <button :class="{ active: mode === 'split' }" type="button" @click="mode = 'split'">{{ t("markdownEditor.split") }}</button>
        <button :class="{ active: mode === 'preview' }" type="button" @click="mode = 'preview'">{{ t("markdownEditor.preview") }}</button>
      </div>
	  <div class="markdown-tools" :aria-label="t('markdownEditor.tools')">
		<button type="button" :title="t('markdownEditor.heading')" @click="prefixSelection('## ')">H2</button>
		<button type="button" :title="t('markdownEditor.bold')" @click="editor && wrapSelection(editor, '**', '**', t('markdownEditor.boldPlaceholder'))"><strong>B</strong></button>
		<button type="button" :title="t('markdownEditor.italic')" @click="editor && wrapSelection(editor, '_', '_', t('markdownEditor.italicPlaceholder'))"><em>I</em></button>
		<button type="button" :title="t('markdownEditor.quote')" @click="prefixSelection('> ')">❯</button>
		<button type="button" :title="t('markdownEditor.code')" @click="editor && wrapSelection(editor, '`', '`', t('markdownEditor.codePlaceholder'))">&lt;/&gt;</button>
		<button type="button" :title="t('markdownEditor.link')" @click="insertLink()">↗</button>
	  </div>
      <span>{{ uploading ? t("markdownEditor.uploading") : t("markdownEditor.imageHelp") }}</span>
      <button v-if="sourceComparison" type="button" :class="{ active: comparisonOpen }" @click="comparisonOpen = !comparisonOpen">{{ comparisonOpen ? t("markdownEditor.hideSource") : t("markdownEditor.showSource") }}</button>
      <button type="button" @click="fullscreen = !fullscreen">{{ fullscreen ? t("markdownEditor.exitFullscreen") : t("markdownEditor.fullscreen") }}</button>
    </div>
    <div class="editor-panes" :class="[`editor-panes--${mode}`, { 'editor-panes--comparison': comparisonOpen && sourceComparison }]">
      <section v-if="comparisonOpen && sourceComparison" class="source-comparison" @scroll="handlePaneScroll"><header>{{ t("markdownEditor.source", { locale: sourceComparison.locale }) }} · {{ sourceComparison.title }}</header><pre>{{ sourceComparison.markdown }}</pre></section>
      <div v-show="mode !== 'preview'" ref="host" class="markdown-source" />
      <article v-show="mode !== 'edit'" ref="previewElement" class="markdown-preview" @scroll="handlePaneScroll" v-html="preview" />
    </div>
  </div>
</template>
