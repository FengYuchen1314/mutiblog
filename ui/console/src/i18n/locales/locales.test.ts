import { describe, expect, test } from "vitest";
import en from "./en";
import zhCN from "./zh-CN";

function keys(value: unknown, prefix = ""): string[] {
  if (!value || typeof value !== "object") return [prefix];
  return Object.entries(value as Record<string, unknown>).flatMap(([key, child]) => keys(child, prefix ? `${prefix}.${key}` : key));
}

describe("console dictionaries", () => {
  test("have the same message keys", () => {
    expect(keys(zhCN).sort()).toEqual(keys(en).sort());
  });
});
