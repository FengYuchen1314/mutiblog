// @vitest-environment jsdom

import { mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";
import { beforeEach, describe, expect, test, vi } from "vitest";
import en from "@/i18n/locales/en";
import MarkdownEditor from "./MarkdownEditor.vue";

const editorDispatch = vi.hoisted(() => vi.fn());
const editorHandlers = vi.hoisted(() => ({
  value: undefined as
    | undefined
    | {
        paste: (event: ClipboardEvent) => boolean;
        drop: (event: DragEvent) => boolean;
      },
}));

type FakeEditorState = {
  doc: { length: number; toString: () => string };
  selection: { main: { from: number; to: number } };
  sliceDoc: () => string;
};

vi.mock("codemirror", () => ({ basicSetup: {} }));
vi.mock("@codemirror/lang-markdown", () => ({ markdown: () => ({}) }));
vi.mock("@codemirror/state", () => ({
  Compartment: class {
    of(value: unknown) {
      return value;
    }

    reconfigure(value: unknown) {
      return value;
    }
  },
  EditorState: {
    readOnly: { of: (value: boolean) => value },
    create: ({ doc }: { doc: string }) => ({
      doc: { length: doc.length, toString: () => doc },
      selection: { main: { from: 0, to: 0 } },
      sliceDoc: () => "",
    }),
  },
}));
vi.mock("@codemirror/view", () => ({
  EditorView: class {
    static lineWrapping = {};
    static editable = { of: (value: boolean) => value };
    static updateListener = { of: (value: unknown) => value };
    static domEventHandlers = (value: NonNullable<typeof editorHandlers.value>) => {
      editorHandlers.value = value;
      return value;
    };

    state: FakeEditorState;
    scrollDOM = document.createElement("div");

    constructor({ parent, state }: { parent?: HTMLElement; state: FakeEditorState }) {
      this.state = state;
      parent?.append(this.scrollDOM);
    }

    dispatch(value: unknown) {
      editorDispatch(value);
    }

    focus() {
      // Test double.
    }

    destroy() {
      // Test double.
    }
  },
  keymap: { of: (value: unknown) => value },
}));
vi.mock("./markdownPreview", () => ({
  createPreviewMarkdownRenderer: () => ({ render: (value: string) => value }),
}));
vi.mock("./mathJaxPreview", () => ({ typesetPreviewMath: vi.fn().mockResolvedValue(undefined) }));

function mountEditor(readonly: boolean) {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    messages: { en: { markdownEditor: en.markdownEditor } },
  });
  return mount(MarkdownEditor, {
    props: { modelValue: "AI translation", readonly },
    global: { plugins: [i18n] },
  });
}

describe("MarkdownEditor read-only mode", () => {
  beforeEach(() => editorDispatch.mockClear());

  test("disables editing tools and rejects imperative insertion", async () => {
    const wrapper = mountEditor(true);

    expect(wrapper.get(".markdown-editor").attributes("aria-readonly")).toBe("true");
    expect(wrapper.findAll(".markdown-tools button")).toHaveLength(6);
    expect(
      wrapper.findAll(".markdown-tools button").every((button) => button.attributes("disabled") !== undefined),
    ).toBe(true);

    (wrapper.vm as unknown as { insertAtCursor: (value: string) => void }).insertAtCursor("\nmanual edit");
    await wrapper.get(".markdown-tools button").trigger("click");
    const preventDefault = vi.fn();
    expect(editorHandlers.value?.paste({ preventDefault } as unknown as ClipboardEvent)).toBe(true);
    expect(editorHandlers.value?.drop({ preventDefault } as unknown as DragEvent)).toBe(true);

    expect(editorDispatch).not.toHaveBeenCalled();
    expect(preventDefault).toHaveBeenCalledTimes(2);
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
    expect(wrapper.emitted("image-files")).toBeUndefined();
    wrapper.unmount();
  });
});
