// @vitest-environment jsdom

import { mount } from "@vue/test-utils";
import { createI18n } from "vue-i18n";
import { describe, expect, test } from "vitest";
import type { ThemeSettingsSchema } from "@/api/client";
import ThemeSettingsField from "./ThemeSettingsField.vue";

function mountField(schema: ThemeSettingsSchema, modelValue: unknown) {
  const i18n = createI18n({
    legacy: false,
    locale: "en",
    messages: { en: {} },
  });
  return mount(ThemeSettingsField, {
    props: { schema, modelValue, label: "Value" },
    global: { plugins: [i18n] },
  });
}

describe("ThemeSettingsField numeric editing", () => {
  test("keeps an empty edit local and restores the controlled number on blur", async () => {
    const wrapper = mountField({ type: "number" }, 7);
    const input = wrapper.get<HTMLInputElement>('input[type="number"]');

    await input.setValue("");

    expect(input.element.value).toBe("");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();

    await input.trigger("blur");

    expect(input.element.value).toBe("7");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
  });

  test.each([
    ["negative", "-12", -12],
    ["decimal", "0.25", 0.25],
  ])("commits a %s number only on blur", async (_case, draft, expected) => {
    const wrapper = mountField({ type: "number" }, 1);
    const input = wrapper.get<HTMLInputElement>('input[type="number"]');

    expect(input.attributes("step")).toBe("any");
    await input.setValue(draft);
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();

    await input.trigger("blur");

    expect(wrapper.emitted("update:modelValue")).toEqual([[expected]]);
    expect(input.element.value).toBe(String(expected));
  });

  test("commits whole integers and rejects a fractional integer edit", async () => {
    const wrapper = mountField({ type: "integer" }, 4);
    const input = wrapper.get<HTMLInputElement>('input[type="number"]');

    expect(input.attributes("step")).toBe("1");
    await input.setValue("-3");
    expect(wrapper.emitted("update:modelValue")).toBeUndefined();
    await input.trigger("blur");
    expect(wrapper.emitted("update:modelValue")).toEqual([[-3]]);

    await wrapper.setProps({ modelValue: -3 });
    await input.setValue("1.5");
    await input.trigger("blur");

    expect(wrapper.emitted("update:modelValue")).toEqual([[-3]]);
    expect(input.element.value).toBe("-3");
  });
});
