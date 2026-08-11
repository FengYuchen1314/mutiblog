export const qwenProviderTemplate = {
  name: "Qwen Free (OpenRouter)",
  kind: "openai-compatible" as const,
  baseUrl: "https://openrouter.ai/api/v1",
  model: "qwen/qwen3-32b:free",
  enabled: true,
  timeoutSeconds: 45,
  maxOutputTokens: 8192,
};

export function preferredProvider<T extends { id: string; default: boolean }>(providers: readonly T[]): T | undefined {
  return providers.find((provider) => provider.id === "qwen-free")
    ?? providers.find((provider) => provider.default)
    ?? providers[0];
}

export function nextQwenProviderId(providers: readonly { id: string }[]): string {
  const ids = new Set(providers.map((provider) => provider.id));
  if (!ids.has("qwen-free")) return "qwen-free";
  for (let suffix = 2; ; suffix += 1) {
    const candidate = `qwen-free-${suffix}`;
    if (!ids.has(candidate)) return candidate;
  }
}
