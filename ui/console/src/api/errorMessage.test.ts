import { describe, expect, test } from "vitest";
import en from "@/i18n/locales/en";
import zhCN from "@/i18n/locales/zh-CN";
import { selectAPIErrorMessage } from "./errorMessage";

const messages: Record<string, string> = {
  "apiErrors.requestFailed": "localized generic error",
  "apiErrors.provider_connection_failed": "localized provider error",
  "apiErrors.provider_invalid": "localized invalid provider",
};
const localization = {
  has: (key: string) => Object.hasOwn(messages, key),
  translate: (key: string) => messages[key] ?? key,
};

describe("API error message selection", () => {
  test("shows the server-sanitized provider connection summary", () => {
    expect(
      selectAPIErrorMessage(
        "provider_connection_failed",
        "  AI provider request failed: HTTP 401: request tenant disabled  ",
        localization,
      ),
    ).toBe("AI provider request failed: HTTP 401: request tenant disabled");
  });

  test("never trusts server messages for other error codes", () => {
    expect(selectAPIErrorMessage("provider_invalid", "untrusted server detail", localization)).toBe(
      "localized invalid provider",
    );
    expect(selectAPIErrorMessage("unknown_error", "untrusted server detail", localization)).toBe(
      "localized generic error",
    );
    expect(selectAPIErrorMessage("provider_connection_failed", "   ", localization)).toBe("localized provider error");
  });

  test("ships localized invalid-provider messages in both console languages", () => {
    expect(en.apiErrors.provider_invalid).toBeTruthy();
    expect(zhCN.apiErrors.provider_invalid).toBeTruthy();
    expect(zhCN.apiErrors.provider_invalid).not.toBe(en.apiErrors.provider_invalid);
  });
});
