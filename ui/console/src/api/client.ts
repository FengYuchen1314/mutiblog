export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields?: Record<string, string>,
  ) {
    super(message);
  }
}

export interface Session {
  username: string;
  csrfToken: string;
  expiresAt: string;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...init.headers },
    ...init,
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new ApiError(response.status, body.code ?? "request_failed", body.message ?? response.statusText, body.fields);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const api = {
  setupStatus: () => request<{ initialized: boolean }>("/api/v1/setup/status"),
  setup: (body: Record<string, string>) =>
    request<{ initialized: boolean; sourceLocale: string; adminLocale: string }>("/api/v1/setup", {
      method: "POST",
      body: JSON.stringify(body),
    }),
  login: (username: string, password: string) =>
    request<Session>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    }),
  session: () => request<Session>("/api/v1/auth/session"),
  logout: (csrfToken: string) =>
    request<void>("/api/v1/auth/logout", { method: "POST", headers: { "X-CSRF-Token": csrfToken } }),
  systemStatus: () =>
    request<{ initialized: boolean; version: string; dataDir: string; index: string; publisher: string }>(
      "/api/v1/admin/system/status",
    ),
};
