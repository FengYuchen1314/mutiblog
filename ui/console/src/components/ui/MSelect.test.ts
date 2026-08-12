// @vitest-environment jsdom

import { mount } from "@vue/test-utils";
import { describe, expect, test } from "vitest";
import MSelect from "./MSelect.vue";

describe("MSelect", () => {
  test("keeps the native control accessible while providing the Material select wrapper", async () => {
    const wrapper = mount(MSelect, {
      attachTo: document.body,
      props: { id: "task-status", modelValue: "queued" },
      slots: { default: "<option value='queued'>Queued</option><option value='failed'>Failed</option>" },
    });

    const select = wrapper.get<HTMLSelectElement>("select");
    select.element.focus();

    expect(document.activeElement).toBe(select.element);
    expect(wrapper.classes()).toContain("m-select");
    expect(select.element.value).toBe("queued");

    await select.setValue("failed");

    expect(wrapper.emitted("update:modelValue")).toEqual([["failed"]]);
    wrapper.unmount();
  });
});
