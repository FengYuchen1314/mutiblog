export interface APIErrorLocalization {
  has: (key: string) => boolean;
  translate: (key: string) => string;
}

export function selectAPIErrorMessage(
  code: string,
  serverMessage: unknown,
  localization: APIErrorLocalization,
): string {
  if (code === "provider_connection_failed" && typeof serverMessage === "string") {
    const safeProviderMessage = serverMessage.trim();
    if (safeProviderMessage) return safeProviderMessage;
  }
  const key = `apiErrors.${code}`;
  return localization.has(key) ? localization.translate(key) : localization.translate("apiErrors.requestFailed");
}
