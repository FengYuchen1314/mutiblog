import { describe, expect, test } from "vitest";
import { nextQwenProviderId, preferredProvider, providerMutationInput, qwenProviderTemplate } from "./providerDefaults";

describe("provider defaults", () => {
  test("prefers the seeded Qwen provider, then the configured default", () => {
    const providers = [
      { id: "deepseek", default: true },
      { id: "qwen-free", default: false },
    ];
    expect(preferredProvider(providers)?.id).toBe("qwen-free");
    expect(
      preferredProvider([
        { id: "deepseek", default: true },
        { id: "other", default: false },
      ])?.id,
    ).toBe("deepseek");
    expect(preferredProvider([])).toBeUndefined();
  });

  test("suggests a non-conflicting Qwen-compatible provider id", () => {
    expect(nextQwenProviderId([])).toBe("qwen-free");
    expect(nextQwenProviderId([{ id: "qwen-free" }])).toBe("qwen-free-2");
    expect(nextQwenProviderId([{ id: "qwen-free" }, { id: "qwen-free-2" }])).toBe("qwen-free-3");
    expect(qwenProviderTemplate.model).toBe("qwen/qwen3-32b:free");
  });

  test("separates ordinary edits from the explicit default promotion", () => {
    const currentDefault = {
      ...qwenProviderTemplate,
      default: true,
      apiKey: "",
      clearKey: false,
    };

    const edit = providerMutationInput(currentDefault, false);
    const promotion = providerMutationInput(currentDefault, true);

    expect(edit.default).toBe(false);
    expect(edit).not.toHaveProperty("apiKey");
    expect(promotion.default).toBe(true);
  });

  test("keeps API key changes explicit and lets clearing win", () => {
    expect(providerMutationInput({ ...qwenProviderTemplate, apiKey: "  new-key  " }, false).apiKey).toBe("new-key");
    expect(providerMutationInput({ ...qwenProviderTemplate, apiKey: "   " }, false)).not.toHaveProperty("apiKey");
    expect(providerMutationInput({ ...qwenProviderTemplate, apiKey: "new-key", clearKey: true }, false).apiKey).toBe(
      "",
    );
  });
});
