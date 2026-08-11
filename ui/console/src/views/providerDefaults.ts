import type { AIProviderInput } from "@/api/client";

export const qwenProviderTemplate = {
  name: "Qwen Free (OpenRouter)",
  kind: "openai-compatible" as const,
  baseUrl: "https://openrouter.ai/api/v1",
  model: "qwen/qwen3-32b:free",
  enabled: true,
  timeoutSeconds: 45,
  maxOutputTokens: 8192,
};

export interface ProviderMutationValues {
  name: string;
  kind: AIProviderInput["kind"];
  baseUrl: string;
  model: string;
  enabled: boolean;
  timeoutSeconds: number | string;
  maxOutputTokens: number | string;
  apiKey?: string;
  clearKey?: boolean;
}

export function providerMutationInput(
  values: ProviderMutationValues,
  promoteToDefault: boolean,
): Omit<AIProviderInput, "id"> {
  const input: Omit<AIProviderInput, "id"> = {
    name: values.name,
    kind: values.kind,
    baseUrl: values.baseUrl,
    model: values.model,
    enabled: values.enabled,
    default: promoteToDefault,
    timeoutSeconds: Number(values.timeoutSeconds),
    maxOutputTokens: Number(values.maxOutputTokens),
  };
  const apiKey = values.apiKey?.trim();
  if (values.clearKey) input.apiKey = "";
  else if (apiKey) input.apiKey = apiKey;
  return input;
}

export function preferredProvider<T extends { id: string; default: boolean }>(providers: readonly T[]): T | undefined {
  return (
    providers.find((provider) => provider.id === "qwen-free") ??
    providers.find((provider) => provider.default) ??
    providers[0]
  );
}

export function nextQwenProviderId(providers: readonly { id: string }[]): string {
  const ids = new Set(providers.map((provider) => provider.id));
  if (!ids.has("qwen-free")) return "qwen-free";
  for (let suffix = 2; ; suffix += 1) {
    const candidate = `qwen-free-${suffix}`;
    if (!ids.has(candidate)) return candidate;
  }
}
