import type { AIProviderInput, AIProviderKind } from "@/api/client";

export const googleFreeProviderTemplate = {
  name: "Google Free Translate",
  kind: "google-free" as const,
  baseUrl: "https://translate.googleapis.com/translate_a/single",
  model: "google-translate",
  enabled: true,
  timeoutSeconds: 45,
  maxOutputTokens: 8192,
};

export const openAICompatibleProviderTemplate = {
  name: "OpenAI-compatible",
  kind: "openai-compatible" as const,
  baseUrl: "",
  model: "",
  enabled: true,
  timeoutSeconds: 45,
  maxOutputTokens: 8192,
};

export function providerTemplate(kind: AIProviderKind) {
  return kind === "google-free" ? googleFreeProviderTemplate : openAICompatibleProviderTemplate;
}

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
  if (values.kind === "google-free") {
    // google-free never needs a credential. Sending an explicit empty value
    // also removes a stale secret when an existing provider changes kind.
    input.apiKey = "";
    return input;
  }
  const apiKey = values.apiKey?.trim();
  if (values.clearKey) input.apiKey = "";
  else if (apiKey) input.apiKey = apiKey;
  return input;
}

export function preferredProvider<T extends { id: string; default: boolean }>(providers: readonly T[]): T | undefined {
  return (
    providers.find((provider) => provider.id === "google-free") ??
    providers.find((provider) => provider.default) ??
    providers[0]
  );
}

export function nextGoogleFreeProviderId(providers: readonly { id: string }[]): string {
  return nextProviderId("google-free", providers);
}

export function nextOpenAICompatibleProviderId(providers: readonly { id: string }[]): string {
  return nextProviderId("openai-compatible", providers);
}

function nextProviderId(base: string, providers: readonly { id: string }[]): string {
  const ids = new Set(providers.map((provider) => provider.id));
  if (!ids.has(base)) return base;
  for (let suffix = 2; ; suffix += 1) {
    const candidate = `${base}-${suffix}`;
    if (!ids.has(candidate)) return candidate;
  }
}
