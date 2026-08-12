import { createPageNavigationLifecycle, qualifiesPageNavigation, type PageNavigationClick } from "@mutiblog/theme-earth";
import { describe, expect, test } from "vitest";

const currentURL = "https://blog.example.test/zh-CN/posts/current/?page=1";

function pageClick(overrides: Partial<PageNavigationClick> = {}): PageNavigationClick {
  return {
    defaultPrevented: false,
    button: 0,
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    hasLink: true,
    href: "https://blog.example.test/zh-CN/posts/next/",
    imageLink: false,
    download: false,
    noPageTransition: false,
    target: null,
    currentURL,
    ...overrides,
  };
}

describe("Earth page navigation indicator", () => {
  test("accepts an ordinary same-origin document navigation", () => {
    expect(qualifiesPageNavigation(pageClick())).toBe(true);
  });

  for (const [name, overrides] of [
    ["a Ctrl-click", { ctrlKey: true }],
    ["a middle-click", { button: 1 }],
    ["a hash-only link", { href: `${currentURL}#comments` }],
    ["a download", { download: true }],
    ["an external link", { href: "https://outside.example.test/next/" }],
    ["a new-tab link", { target: "_blank" }],
  ] satisfies Array<[string, Partial<PageNavigationClick>]>) {
    test(`does not start for ${name}`, () => {
      expect(qualifiesPageNavigation(pageClick(overrides))).toBe(false);
    });
  }

  test("clears a pending indicator when pageshow restores a page", () => {
    const calls: string[] = [];
    const scheduled = { id: "navigation-timeout" };
    const cancelled: unknown[] = [];
    const lifecycle = createPageNavigationLifecycle({
      qualifies: qualifiesPageNavigation,
      onPending: () => calls.push("pending"),
      onReset: () => calls.push("reset"),
      schedule: () => scheduled,
      cancel: (timer) => cancelled.push(timer),
    });

    lifecycle.handleClick(pageClick());
    expect(lifecycle.isPending()).toBe(true);
    expect(calls).toEqual(["pending"]);

    lifecycle.handlePageShow();
    expect(lifecycle.isPending()).toBe(false);
    expect(cancelled).toEqual([scheduled]);
    expect(calls).toEqual(["pending", "reset"]);
  });
});
