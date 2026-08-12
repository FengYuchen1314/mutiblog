// @vitest-environment jsdom

import { mount } from "@vue/test-utils";
import { describe, expect, test } from "vitest";
import MButton from "./MButton.vue";

describe("MButton", () => {
  test("provides a native focus target and emits clicks", async () => {
    const wrapper = mount(MButton, {
      attachTo: document.body,
      props: { variant: "filled" },
      slots: { default: "Save" },
    });
    const button = wrapper.get<HTMLButtonElement>("button");

    button.element.focus();

    expect(document.activeElement).toBe(button.element);
    expect(button.classes()).toContain("m-button--filled");

    await button.trigger("click");

    expect(wrapper.emitted("click")).toHaveLength(1);
    wrapper.unmount();
  });

  test("keeps legacy Halo visual props available during migration", () => {
    const wrapper = mount(MButton, {
      props: { type: "secondary", size: "xs", circle: true },
      slots: { icon: "×", default: "Close" },
    });

    expect(wrapper.classes()).toEqual(
      expect.arrayContaining(["m-button--outlined", "m-button--xs", "m-button--circle"]),
    );
    wrapper.unmount();
  });
});
