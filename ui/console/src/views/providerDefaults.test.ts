import { describe, expect, test } from "vitest";
import {
  googleFreeProviderTemplate,
  nextGoogleFreeProviderId,
  nextOpenAICompatibleProviderId,
  openAICompatibleProviderTemplate,
  preferredProvider,
  providerMutationInput,
  providerTemplate,
} from "./providerDefaults";

describe("provider defaults", () => {
  test("prefers the seeded Google provider, then the configured default", () => {
    const providers = [
      { id: "deepseek", default: true },
      { id: "google-free", default: false },
    ];
    expect(preferredProvider(providers)?.id).toBe("google-free");
    expect(
      preferredProvider([
        { id: "deepseek", default: true },
        { id: "other", default: false },
      ])?.id,
    ).toBe("deepseek");
    expect(preferredProvider([])).toBeUndefined();
  });

  test("suggests a non-conflicting Google provider id and keeps both templates", () => {
    expect(nextGoogleFreeProviderId([])).toBe("google-free");
    expect(nextGoogleFreeProviderId([{ id: "google-free" }])).toBe("google-free-2");
    expect(nextGoogleFreeProviderId([{ id: "google-free" }, { id: "google-free-2" }])).toBe("google-free-3");
    expect(nextOpenAICompatibleProviderId([])).toBe("openai-compatible");
    expect(nextOpenAICompatibleProviderId([{ id: "openai-compatible" }])).toBe("openai-compatible-2");
    expect(providerTemplate("google-free")).toBe(googleFreeProviderTemplate);
    expect(providerTemplate("openai-compatible")).toBe(openAICompatibleProviderTemplate);
    expect(googleFreeProviderTemplate).toEqual({
      name: "Google Free Translate",
      kind: "google-free",
      baseUrl: "https://translate.googleapis.com/translate_a/single",
      model: "google-translate",
      enabled: true,
      timeoutSeconds: 45,
      maxOutputTokens: 8192,
    });
  });

  test("separates ordinary edits from the explicit default promotion", () => {
    const currentDefault = {
      ...openAICompatibleProviderTemplate,
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
    expect(providerMutationInput({ ...openAICompatibleProviderTemplate, apiKey: "  new-key  " }, false).apiKey).toBe(
      "new-key",
    );
    expect(providerMutationInput({ ...openAICompatibleProviderTemplate, apiKey: "   " }, false)).not.toHaveProperty(
      "apiKey",
    );
    expect(
      providerMutationInput({ ...openAICompatibleProviderTemplate, apiKey: "new-key", clearKey: true }, false).apiKey,
    ).toBe("");
  });

  test("never sends an API key for Google free translation", () => {
    expect(providerMutationInput({ ...googleFreeProviderTemplate, apiKey: "stale-key" }, true)).toMatchObject({
      kind: "google-free",
      default: true,
      apiKey: "",
    });
  });
});
