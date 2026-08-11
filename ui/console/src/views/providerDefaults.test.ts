import { describe, expect, test } from "vitest";
import { nextQwenProviderId, preferredProvider, qwenProviderTemplate } from "./providerDefaults";

describe("provider defaults", () => {
  test("prefers the seeded Qwen provider, then the configured default", () => {
    const providers = [
      { id: "deepseek", default: true },
      { id: "qwen-free", default: false },
    ];
    expect(preferredProvider(providers)?.id).toBe("qwen-free");
    expect(preferredProvider([{ id: "deepseek", default: true }, { id: "other", default: false }])?.id).toBe("deepseek");
    expect(preferredProvider([])).toBeUndefined();
  });

  test("suggests a non-conflicting Qwen-compatible provider id", () => {
    expect(nextQwenProviderId([])).toBe("qwen-free");
    expect(nextQwenProviderId([{ id: "qwen-free" }])).toBe("qwen-free-2");
    expect(nextQwenProviderId([{ id: "qwen-free" }, { id: "qwen-free-2" }])).toBe("qwen-free-3");
    expect(qwenProviderTemplate.model).toBe("qwen/qwen3-32b:free");
  });
});
