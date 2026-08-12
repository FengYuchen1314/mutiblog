import { i18n } from "@/i18n";
import { selectAPIErrorMessage } from "./errorMessage";

export const AUTH_EXPIRED_EVENT = "mutiblog:auth-expired";
export const PUBLICATION_FAILED_EVENT = "mutiblog:publication-failed";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fields?: Record<string, string>,
    public details?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export interface Session {
  username: string;
  csrfToken: string;
  expiresAt: string;
  adminLocale: "en" | "zh-CN";
}

export interface LocalizedMarkdown {
  title: string;
  summary?: string;
  seoTitle?: string;
  seoDescription?: string;
  markdown: string;
}

export interface Post {
  meta: {
    id: string;
    status: "draft" | "published" | "unpublished" | "recycled";
    sourceLocale: string;
    createdAt: string;
    updatedAt: string;
    publishedAt?: string;
    revision: number;
    baseRevision: number;
    headRevision: number;
    releaseRevision?: number;
    scheduledRevision?: number;
    hasUnpublishedChanges: boolean;
    categories: string[];
    tags: string[];
    cover?: string;
    pinned: boolean;
    visibility: "public" | "private";
    commentPolicy: "open" | "closed";
    template: string;
    locales: Record<
      string,
      { state: "current" | "stale"; origin: "source" | "ai" | "manual"; revision: number; sourceRevision: number }
    >;
  };
  content: Record<string, LocalizedMarkdown>;
}

export interface LocalesConfig {
  sourceLocale: string;
  enabled: Array<{
    code: string;
    label: string;
    enabled: boolean;
    status?: "provisioning" | "building" | "ready" | "failed";
  }>;
  fallback: string[];
}

export interface FrameworkDictionary {
  locale: string;
  translated: number;
  total: number;
  missing: string[];
  values: Record<string, string>;
}

export interface MediaAsset {
  schemaVersion: number;
  kind: "MediaAsset";
  id: string;
  filename: string;
  originalName: string;
  mimeType: string;
  size: number;
  url: string;
  createdAt: string;
}

export interface Taxonomy {
  schemaVersion: number;
  kind: "Category" | "Tag";
  id: string;
  sourceLocale: string;
  parentId?: string;
  cover?: string;
  template: string;
  revision: number;
  createdAt: string;
  updatedAt: string;
  locales: Record<
    string,
    {
      name: string;
      description?: string;
      seoTitle?: string;
      seoDescription?: string;
      state: "current" | "stale";
      origin: "source" | "manual" | "ai";
      revision: number;
      sourceRevision: number;
    }
  >;
}

export interface CommentRecord {
  id: string;
  subject: { kind: "Post" | "Page"; id: string };
  parentId?: string;
  status: "pending" | "approved" | "spam";
  author: { name: string; website?: string };
  content: string;
  locale: string;
  createdAt: string;
}

export interface LocalizedLink {
  name: string;
  description?: string;
  state: "current" | "stale";
  origin: "source" | "manual" | "ai";
  revision: number;
  sourceRevision: number;
}
export interface LinkGroup {
  id: string;
  sourceLocale: string;
  order: number;
  revision: number;
  locales: Record<string, LocalizedLink>;
}
export interface SiteLink {
  id: string;
  groupId: string;
  url: string;
  logo?: string;
  order: number;
  sourceLocale: string;
  revision: number;
  locales: Record<string, LocalizedLink>;
}
export interface MenuItem {
  id: string;
  parentId?: string;
  targetKind: "internal" | "external";
  url: string;
  openInNew: boolean;
  order: number;
  locales: Record<
    string,
    { label: string; state: string; origin: "source" | "manual" | "ai"; revision: number; sourceRevision: number }
  >;
}
export interface MenuRecord {
  id: string;
  sourceLocale: string;
  revision: number;
  locales: Record<
    string,
    { label: string; state: string; origin: "source" | "manual" | "ai"; revision: number; sourceRevision: number }
  >;
  items: MenuItem[];
}
export interface BackupRecord {
  id: string;
  filename: string;
  size: number;
  createdAt: string;
}
export interface TaskProgress {
  phase: string;
  current: number;
  total: number;
  percent: number;
  message?: string;
}
export type TaskStatus = "queued" | "running" | "succeeded" | "failed" | "needs-review";
export interface BackupTask {
  id: string;
  kind: "Backup";
  operation: "create" | "import-local" | "import-url" | "restore";
  status: Exclude<TaskStatus, "needs-review">;
  progress: TaskProgress;
  backupId?: string;
  error?: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
}
export interface ContentRevision {
  id: string;
  revision: number;
  createdAt: string;
  status: Post["meta"]["status"];
  title: string;
  isBase: boolean;
  isHead: boolean;
  isRelease: boolean;
}
export interface IndexStats {
  status: "building" | "ready" | "error";
  documents: number;
  path: string;
  lastIndexedAt?: string;
  lastExternalChangeAt?: string;
  lastError?: string;
}
export interface ThemeContentTemplate {
  id: string;
  name: string;
}
export interface ThemeCondition {
  type: "Compatible";
  status: boolean;
  reason: string;
  message: string;
}
export interface ThemeRecord {
  schemaVersion: number;
  id: string;
  name: string;
  version: string;
  requires?: string;
  engine: "react-ssr";
  server: string;
  assets?: string;
  screenshot?: string;
  screenshotUrl?: string;
  settingsSchema?: string;
  settingsReload?: "rebuild";
  postTemplates?: ThemeContentTemplate[];
  pageTemplates?: ThemeContentTemplate[];
  categoryTemplates?: ThemeContentTemplate[];
  active: boolean;
  builtIn: boolean;
  status: "ready" | "incompatible";
  conditions: ThemeCondition[];
}
export interface ThemePreview {
  schemaVersion: number;
  id: string;
  themeId: string;
  createdAt: string;
  expiresAt: string;
  report: StaticBuildReport;
  url: string;
}
export interface ThemeSettingsSchema {
  title?: string;
  "x-i18n"?: Record<string, string>;
  "x-enum-i18n"?: Record<string, Record<string, string>>;
  type: "object" | "array" | "string" | "boolean" | "number" | "integer";
  format?: string;
  enum?: string[];
  default?: unknown;
  minItems?: number;
  maxItems?: number;
  minLength?: number;
  maxLength?: number;
  properties?: Record<string, ThemeSettingsSchema>;
  items?: ThemeSettingsSchema;
}
export interface ThemeSettings {
  themeId: string;
  active: boolean;
  schema: ThemeSettingsSchema;
  values: Record<string, unknown>;
}
export interface SiteSettings {
  sourceLocale: string;
  adminLocale: "en" | "zh-CN";
  timezone: string;
  baseUrl?: string;
  logo?: string;
  activeTheme: string;
  primaryMenu?: string;
  idStrategy: "uuid" | "timestamp";
  locales: Record<string, { title: string; subtitle?: string; description?: string }>;
}
export interface CommentSettings {
  moderation: "pending" | "none";
  pageSize: number;
  maxLength: number;
}
export interface SecurityAuditEvent {
  time: string;
  action:
    | "setup"
    | "login"
    | "logout"
    | "password-change"
    | "backup-import"
    | "backup-restore"
    | "theme-install"
    | "theme-activate"
    | "theme-reload"
    | "theme-preview"
    | "theme-uninstall"
    | "theme-uninstall-settings"
    | "theme-settings"
    | "provider-update"
    | "provider-delete"
    | "media-delete";
  outcome: "queued" | "succeeded" | "failed" | "rejected" | "rate-limited";
  actor?: string;
  clientHash?: string;
}

export interface StaticBuildReport {
  schemaVersion: number;
  generatedAt: string;
  files: number;
  locales: string[];
  redirects: Array<{ from: string; to: string; status: 302 }>;
}

export interface PublishResult {
  post: Post;
  build: {
    status: "succeeded" | "failed" | "scheduled" | "deferred" | "blocked";
    report?: StaticBuildReport;
    taskId?: string;
    dueAt?: string;
  };
  translation: {
    status: "queued" | "running" | "not-needed" | "not-configured" | "failed" | "deferred";
    taskId?: string;
  };
}

export interface TranslationTask {
  schemaVersion: number;
  id: string;
  kind: "Translation";
  entityKind: "Post" | "Page";
  entityId: string;
  sourceLocale: string;
  sourceRevision: number;
  providerId: string;
  model: string;
  overwriteManual: boolean;
  status: TaskStatus;
  progress: TaskProgress;
  targets: Array<{
    locale: string;
    expectedRevision: number;
    status: string;
    progress: TaskProgress;
    attempts: number;
    error?: string;
    completedAt?: string;
  }>;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  error?: string;
}

export type TaskKind =
  "Translation" | "StaticBuild" | "Backup" | "ScheduledPublish" | "IndexRebuild" | "LocaleProvision";

export type TaskRelationRole = "parent" | "static-build" | "translation";

export interface TaskRelation {
  id: string;
  role: TaskRelationRole;
}

// Tasks outside the per-content translation worker can still report progress
// for each target locale. Keep the task-center shape intentionally small so
// the durable task API is not coupled to translation-only receipt fields.
export interface TaskTarget {
  locale: string;
  status: string;
  progress?: TaskProgress;
  attempts?: number;
  expectedRevision?: number;
  error?: string;
  errorDetail?: string;
  startedAt?: string;
  completedAt?: string;
}

export interface UnifiedTask {
  schemaVersion: number;
  id: string;
  kind: TaskKind;
  operation: string;
  subject?: { kind: string; id: string };
  parentTaskId?: string;
  status: TaskStatus;
  progress: TaskProgress;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
  error?: string;
  errorDetail?: string;
  providerId?: string;
  model?: string;
  backupId?: string;
  dueAt?: string;
  buildStatus?: string;
  buildTaskId?: string;
  translationStatus?: string;
  translationTaskId?: string;
  outcome?: string;
  targets?: TaskTarget[];
  relations?: TaskRelation[];
  report?: StaticBuildReport;
}

export function createStaticBuildTaskId(now = new Date()): string {
  const iso = now.toISOString();
  const timestamp = `${iso.slice(0, 4)}${iso.slice(5, 7)}${iso.slice(8, 10)}T${iso.slice(11, 13)}${iso.slice(14, 16)}${iso.slice(17, 19)}.${iso.slice(20, 23)}000000Z`;
  const random = new Uint8Array(4);
  crypto.getRandomValues(random);
  return `${timestamp}-${Array.from(random, (value) => value.toString(16).padStart(2, "0")).join("")}`;
}

export function createBackupTaskId(now = new Date()): string {
  return `backup-task-${createStaticBuildTaskId(now)}`;
}

export function createIndexRebuildTaskId(now = new Date()): string {
  return `index-rebuild-${createStaticBuildTaskId(now)}`;
}

function trackedMutationHeaders(csrfToken: string, taskId?: string): Record<string, string> {
  return taskId ? { "X-CSRF-Token": csrfToken, "X-MutiBlog-Task-ID": taskId } : { "X-CSRF-Token": csrfToken };
}

export type AIProviderKind = "google-free" | "openai-compatible";

export interface AIProvider {
  id: string;
  name: string;
  kind: AIProviderKind;
  baseUrl: string;
  model: string;
  enabled: boolean;
  timeoutSeconds: number;
  maxOutputTokens: number;
  default: boolean;
  hasKey: boolean;
  maskedKey?: string;
}

export type AIProviderInput = Omit<AIProvider, "hasKey" | "maskedKey"> & { apiKey?: string };

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !(init.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  let response: Response;
  try {
    response = await fetch(path, {
      credentials: "same-origin",
      ...init,
      headers,
    });
  } catch (caught) {
    if (caught instanceof Error && caught.name === "AbortError") throw caught;
    throw new ApiError(0, "network_unavailable", String(i18n.global.t("apiErrors.network_unavailable")));
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    const code = body.code ?? "request_failed";
    const message = selectAPIErrorMessage(code, body.message, {
      has: (key) => i18n.global.te(key),
      translate: (key) => String(i18n.global.t(key)),
    });
    if (
      typeof window !== "undefined" &&
      ["authentication_required", "session_expired", "invalid_csrf"].includes(code)
    ) {
      window.dispatchEvent(new CustomEvent(AUTH_EXPIRED_EVENT, { detail: { code } }));
    }
    throw new ApiError(response.status, code, message, body.fields, body);
  }
  if (response.status === 204) return undefined as T;
  const result = (await response.json()) as T;
  if (typeof window !== "undefined" && response.headers.get("X-MutiBlog-Static-Build") === "failed") {
    window.dispatchEvent(new CustomEvent(PUBLICATION_FAILED_EVENT));
  }
  return result;
}

export const api = {
  setupStatus: () => request<{ initialized: boolean }>("/api/v1/setup/status"),
  setup: (body: Record<string, string>, taskId?: string) =>
    request<{
      initialized: boolean;
      sourceLocale: string;
      adminLocale: string;
      session: Session;
      build: { status: "queued" | "succeeded" | "failed"; taskId?: string; report?: StaticBuildReport };
    }>("/api/v1/setup", {
      method: "POST",
      headers: taskId ? { "X-MutiBlog-Task-ID": taskId } : undefined,
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
    request<{ initialized: boolean; version: string; dataDir: string; index: IndexStats; publisher: string }>(
      "/api/v1/admin/system/status",
    ),
  securityAudit: (limit = 20) =>
    request<{ items: SecurityAuditEvent[] }>(`/api/v1/admin/security/audit?limit=${limit}`),
  indexStatus: () => request<IndexStats>("/api/v1/admin/index/status"),
  rebuildIndex: (csrfToken: string, taskId?: string) =>
    request<{ task: UnifiedTask }>("/api/v1/admin/index/rebuild", {
      method: "POST",
      headers: taskId
        ? { "X-CSRF-Token": csrfToken, "X-MutiBlog-Index-Task-ID": taskId }
        : { "X-CSRF-Token": csrfToken },
    }),
  searchIndex: (query: string, limit = 20) =>
    request<{
      items: Array<{
        kind: "Post" | "Page";
        id: string;
        locale: string;
        status: string;
        title: string;
        summary?: string;
      }>;
      total: number;
    }>(`/api/v1/admin/index/search?q=${encodeURIComponent(query)}&limit=${limit}`),
  locales: () => request<LocalesConfig>("/api/v1/admin/locales"),
  updateLocales: (csrfToken: string, enabled: LocalesConfig["enabled"], sourceLocale: string) =>
    request<{
      locales: LocalesConfig;
      task?: UnifiedTask;
      localization: { status: "queued" | "idle"; taskId?: string };
      build: { status: "deferred" | "skipped" };
    }>("/api/v1/admin/locales", {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify({ enabled, sourceLocale }),
    }),
  dictionaries: () => request<{ items: FrameworkDictionary[] }>("/api/v1/admin/dictionaries"),
  updateDictionary: (csrfToken: string, locale: string, values: Record<string, string>, taskId?: string) =>
    request<FrameworkDictionary>(`/api/v1/admin/dictionaries/${encodeURIComponent(locale)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ values }),
    }),
  posts: () => request<{ page: number; size: number; total: number; items: Post[] }>("/api/v1/admin/posts"),
  post: (id: string) => request<Post>(`/api/v1/admin/posts/${encodeURIComponent(id)}`),
  createPost: (
    csrfToken: string,
    body: {
      id?: string;
      title: string;
      summary?: string;
      seoTitle?: string;
      seoDescription?: string;
      markdown: string;
    },
  ) =>
    request<Post>("/api/v1/admin/posts", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  updatePostLocale: (csrfToken: string, id: string, locale: string, body: LocalizedMarkdown & { revision: number }) =>
    request<Post>(`/api/v1/admin/posts/${encodeURIComponent(id)}/locales/${encodeURIComponent(locale)}`, {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  updatePostSettings: (
    csrfToken: string,
    id: string,
    body: {
      revision: number;
      categories: string[];
      tags: string[];
      cover: string;
      pinned: boolean;
      visibility: "public" | "private";
      publishedAt: string;
      commentPolicy: "open" | "closed";
      template: string;
    },
  ) =>
    request<Post>(`/api/v1/admin/posts/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  publishPost: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<PublishResult>(`/api/v1/admin/posts/${encodeURIComponent(id)}/publish`, {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  postRevisions: (id: string) =>
    request<{ items: ContentRevision[]; total: number }>(`/api/v1/admin/posts/${encodeURIComponent(id)}/revisions`),
  postRevision: (id: string, revisionId: string) =>
    request<Post>(`/api/v1/admin/posts/${encodeURIComponent(id)}/revisions/${encodeURIComponent(revisionId)}`),
  restorePostRevision: (csrfToken: string, id: string, revisionId: string, revision: number) =>
    request<{ post: Post; build: { status: "not-needed" } }>(
      `/api/v1/admin/posts/${encodeURIComponent(id)}/revisions/${encodeURIComponent(revisionId)}/restore`,
      { method: "POST", headers: { "X-CSRF-Token": csrfToken }, body: JSON.stringify({ revision }) },
    ),
  changePostStatus: (
    csrfToken: string,
    id: string,
    action: "unpublish" | "recycle" | "restore",
    revision: number,
    taskId?: string,
  ) =>
    request<{ post: Post; build: { status: "succeeded" | "failed" } }>(
      `/api/v1/admin/posts/${encodeURIComponent(id)}/${action}`,
      { method: "POST", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify({ revision }) },
    ),
  deletePost: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<{ deleted: true; build: { status: "succeeded" | "failed" } }>(
      `/api/v1/admin/posts/${encodeURIComponent(id)}`,
      { method: "DELETE", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify({ revision }) },
    ),
  pages: () => request<{ page: number; size: number; total: number; items: Post[] }>("/api/v1/admin/pages"),
  page: (id: string) => request<Post>(`/api/v1/admin/pages/${encodeURIComponent(id)}`),
  createPage: (
    csrfToken: string,
    body: {
      id?: string;
      title: string;
      summary?: string;
      seoTitle?: string;
      seoDescription?: string;
      markdown: string;
    },
  ) =>
    request<Post>("/api/v1/admin/pages", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  updatePageLocale: (csrfToken: string, id: string, locale: string, body: LocalizedMarkdown & { revision: number }) =>
    request<Post>(`/api/v1/admin/pages/${encodeURIComponent(id)}/locales/${encodeURIComponent(locale)}`, {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  updatePageSettings: (
    csrfToken: string,
    id: string,
    body: {
      revision: number;
      cover: string;
      visibility: "public" | "private";
      publishedAt: string;
      commentPolicy: "open" | "closed";
      template: string;
    },
  ) =>
    request<Post>(`/api/v1/admin/pages/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  publishPage: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<PublishResult>(`/api/v1/admin/pages/${encodeURIComponent(id)}/publish`, {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  pageRevisions: (id: string) =>
    request<{ items: ContentRevision[]; total: number }>(`/api/v1/admin/pages/${encodeURIComponent(id)}/revisions`),
  pageRevision: (id: string, revisionId: string) =>
    request<Post>(`/api/v1/admin/pages/${encodeURIComponent(id)}/revisions/${encodeURIComponent(revisionId)}`),
  restorePageRevision: (csrfToken: string, id: string, revisionId: string, revision: number) =>
    request<{ post: Post; build: { status: "not-needed" } }>(
      `/api/v1/admin/pages/${encodeURIComponent(id)}/revisions/${encodeURIComponent(revisionId)}/restore`,
      { method: "POST", headers: { "X-CSRF-Token": csrfToken }, body: JSON.stringify({ revision }) },
    ),
  changePageStatus: (
    csrfToken: string,
    id: string,
    action: "unpublish" | "recycle" | "restore",
    revision: number,
    taskId?: string,
  ) =>
    request<{ post: Post; build: { status: "succeeded" | "failed" } }>(
      `/api/v1/admin/pages/${encodeURIComponent(id)}/${action}`,
      { method: "POST", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify({ revision }) },
    ),
  deletePage: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<{ deleted: true; build: { status: "succeeded" | "failed" } }>(
      `/api/v1/admin/pages/${encodeURIComponent(id)}`,
      { method: "DELETE", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify({ revision }) },
    ),
  rebuildSite: (csrfToken: string, taskId?: string) =>
    request<{ status: "succeeded"; report: StaticBuildReport }>("/api/v1/admin/publish/build", {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
    }),
  taxonomies: (kind: "categories" | "tags") =>
    request<{ items: Taxonomy[]; total: number }>(`/api/v1/admin/taxonomies/${kind}`),
  createTaxonomy: (
    csrfToken: string,
    kind: "categories" | "tags",
    body: { id?: string; name: string; description?: string; parentId?: string; cover?: string; template?: string },
    taskId?: string,
  ) =>
    request<Taxonomy>(`/api/v1/admin/taxonomies/${kind}`, {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateTaxonomyStructure: (
    csrfToken: string,
    kind: "categories" | "tags",
    id: string,
    revision: number,
    body: { parentId?: string; cover?: string; template?: string },
    taskId?: string,
  ) =>
    request<Taxonomy>(`/api/v1/admin/taxonomies/${kind}/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({
        revision,
        parentId: body.parentId ?? "",
        cover: body.cover ?? "",
        template: body.template ?? "",
      }),
    }),
  updateTaxonomyLocale: (
    csrfToken: string,
    kind: "categories" | "tags",
    id: string,
    locale: string,
    body: { revision: number; name: string; description?: string; seoTitle?: string; seoDescription?: string },
    taskId?: string,
  ) =>
    request<Taxonomy>(
      `/api/v1/admin/taxonomies/${kind}/${encodeURIComponent(id)}/locales/${encodeURIComponent(locale)}`,
      { method: "PUT", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify(body) },
    ),
  deleteTaxonomy: (csrfToken: string, kind: "categories" | "tags", id: string, revision: number, taskId?: string) =>
    request<void>(`/api/v1/admin/taxonomies/${kind}/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  attachments: () =>
    request<{ page: number; size: number; total: number; items: MediaAsset[] }>("/api/v1/admin/attachments"),
  uploadAttachment: (csrfToken: string, file: File) => {
    const body = new FormData();
    body.append("file", file, file.name);
    return request<MediaAsset>("/api/v1/admin/attachments", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
      body,
    });
  },
  deleteAttachment: (csrfToken: string, id: string) =>
    request<void>(`/api/v1/admin/attachments/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: { "X-CSRF-Token": csrfToken },
    }),
  providers: () => request<{ items: AIProvider[] }>("/api/v1/admin/ai/providers"),
  saveProvider: (csrfToken: string, id: string, body: Omit<AIProviderInput, "id">) =>
    request<AIProvider>(`/api/v1/admin/ai/providers/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
  deleteProvider: (csrfToken: string, id: string) =>
    request<void>(`/api/v1/admin/ai/providers/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: { "X-CSRF-Token": csrfToken },
    }),
  testProvider: (csrfToken: string, id: string) =>
    request<{ providerId: string; model: string; latencyMs: number; response: string }>(
      `/api/v1/admin/ai/providers/${encodeURIComponent(id)}/test`,
      {
        method: "POST",
        headers: { "X-CSRF-Token": csrfToken },
      },
    ),
  tasks: (query: { kind?: UnifiedTask["kind"]; status?: TaskStatus; limit?: number } = {}, signal?: AbortSignal) => {
    const parameters = new URLSearchParams();
    if (query.kind) parameters.set("kind", query.kind);
    if (query.status) parameters.set("status", query.status);
    if (query.limit) parameters.set("limit", String(query.limit));
    const serialized = parameters.toString();
    const suffix = serialized ? `?${serialized}` : "";
    return request<{ items: UnifiedTask[]; total: number; active: number }>(`/api/v1/admin/tasks${suffix}`, { signal });
  },
  task: (id: string, signal?: AbortSignal) =>
    request<UnifiedTask>(`/api/v1/admin/tasks/${encodeURIComponent(id)}`, { signal }),
  comments: (query: { status?: string; kind?: string; q?: string; page?: number; size?: number } = {}) => {
    const parameters = new URLSearchParams();
    for (const [key, value] of Object.entries(query))
      if (value !== undefined && value !== "") parameters.set(key, String(value));
    return request<{ page: number; size: number; items: CommentRecord[]; total: number }>(
      `/api/v1/admin/comments?${parameters}`,
    );
  },
  moderateComment: (csrfToken: string, comment: CommentRecord, status: CommentRecord["status"]) =>
    request<CommentRecord>(
      `/api/v1/admin/comments/${comment.subject.kind}/${encodeURIComponent(comment.subject.id)}/${encodeURIComponent(comment.id)}`,
      { method: "PUT", headers: { "X-CSRF-Token": csrfToken }, body: JSON.stringify({ status }) },
    ),
  deleteComment: (csrfToken: string, comment: CommentRecord) =>
    request<void>(
      `/api/v1/admin/comments/${comment.subject.kind}/${encodeURIComponent(comment.subject.id)}/${encodeURIComponent(comment.id)}`,
      { method: "DELETE", headers: { "X-CSRF-Token": csrfToken } },
    ),
  links: () => request<{ groups: LinkGroup[]; items: SiteLink[] }>("/api/v1/admin/links"),
  createLinkGroup: (
    csrfToken: string,
    body: { id?: string; name: string; description?: string; order?: number },
    taskId?: string,
  ) =>
    request<LinkGroup>("/api/v1/admin/links/groups", {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  createLink: (
    csrfToken: string,
    body: {
      id?: string;
      groupId: string;
      url: string;
      logo?: string;
      name: string;
      description?: string;
      order?: number;
    },
    taskId?: string,
  ) =>
    request<SiteLink>("/api/v1/admin/links/items", {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateLinkGroup: (csrfToken: string, id: string, revision: number, order: number, taskId?: string) =>
    request<LinkGroup>(`/api/v1/admin/links/groups/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision, order }),
    }),
  updateLink: (
    csrfToken: string,
    id: string,
    body: { revision: number; groupId: string; url: string; logo?: string; order: number },
    taskId?: string,
  ) =>
    request<SiteLink>(`/api/v1/admin/links/items/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateLinkGroupLocale: (
    csrfToken: string,
    id: string,
    locale: string,
    body: { revision: number; name: string; description?: string },
    taskId?: string,
  ) =>
    request<LinkGroup>(`/api/v1/admin/links/groups/${encodeURIComponent(id)}/locales/${encodeURIComponent(locale)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateLinkLocale: (
    csrfToken: string,
    id: string,
    locale: string,
    body: { revision: number; name: string; description?: string },
    taskId?: string,
  ) =>
    request<SiteLink>(`/api/v1/admin/links/items/${encodeURIComponent(id)}/locales/${encodeURIComponent(locale)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  deleteLinkGroup: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<void>(`/api/v1/admin/links/groups/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  deleteLink: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<void>(`/api/v1/admin/links/items/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  menus: () => request<{ items: MenuRecord[] }>("/api/v1/admin/menus"),
  createMenu: (csrfToken: string, body: { id?: string; label: string }, taskId?: string) =>
    request<MenuRecord>("/api/v1/admin/menus", {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  addMenuItem: (
    csrfToken: string,
    menuId: string,
    body: {
      id?: string;
      parentId?: string;
      targetKind: "internal" | "external";
      url: string;
      label: string;
      openInNew: boolean;
      order: number;
      revision: number;
    },
    taskId?: string,
  ) =>
    request<MenuRecord>(`/api/v1/admin/menus/${encodeURIComponent(menuId)}/items`, {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateMenuItem: (
    csrfToken: string,
    menuId: string,
    itemId: string,
    body: {
      revision: number;
      parentId?: string;
      targetKind: "internal" | "external";
      url: string;
      openInNew: boolean;
      order: number;
    },
    taskId?: string,
  ) =>
    request<MenuRecord>(`/api/v1/admin/menus/${encodeURIComponent(menuId)}/items/${encodeURIComponent(itemId)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateMenuLocale: (
    csrfToken: string,
    id: string,
    locale: string,
    body: { revision: number; label: string },
    taskId?: string,
  ) =>
    request<MenuRecord>(`/api/v1/admin/menus/${encodeURIComponent(id)}/locales/${encodeURIComponent(locale)}`, {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateMenuItemLocale: (
    csrfToken: string,
    menuId: string,
    itemId: string,
    locale: string,
    body: { revision: number; label: string },
    taskId?: string,
  ) =>
    request<MenuRecord>(
      `/api/v1/admin/menus/${encodeURIComponent(menuId)}/items/${encodeURIComponent(itemId)}/locales/${encodeURIComponent(locale)}`,
      { method: "PUT", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify(body) },
    ),
  deleteMenu: (csrfToken: string, id: string, revision: number, taskId?: string) =>
    request<void>(`/api/v1/admin/menus/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  deleteMenuItem: (csrfToken: string, menuId: string, itemId: string, revision: number, taskId?: string) =>
    request<MenuRecord>(`/api/v1/admin/menus/${encodeURIComponent(menuId)}/items/${encodeURIComponent(itemId)}`, {
      method: "DELETE",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ revision }),
    }),
  backups: () => request<{ items: BackupRecord[] }>("/api/v1/admin/backups"),
  backupTasks: () => request<{ items: BackupTask[] }>("/api/v1/admin/backups/tasks"),
  createBackup: (csrfToken: string) =>
    request<BackupTask>("/api/v1/admin/backups", { method: "POST", headers: { "X-CSRF-Token": csrfToken } }),
  importBackup: (csrfToken: string, file: File) => {
    const body = new FormData();
    body.append("file", file, file.name);
    return request<BackupTask>("/api/v1/admin/backups/import", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
      body,
    });
  },
  importBackupURL: (csrfToken: string, url: string) =>
    request<BackupTask>("/api/v1/admin/backups/import-url", {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify({ url }),
    }),
  restoreBackup: (csrfToken: string, id: string, taskId?: string) =>
    request<{
      status: "succeeded";
      health: "ready";
      report: StaticBuildReport;
      safetyBackup: BackupRecord;
      reauthenticate: true;
    }>(`/api/v1/admin/backups/${encodeURIComponent(id)}/restore`, {
      method: "POST",
      headers: taskId
        ? { "X-CSRF-Token": csrfToken, "X-MutiBlog-Backup-Task-ID": taskId }
        : { "X-CSRF-Token": csrfToken },
    }),
  deleteBackup: (csrfToken: string, id: string) =>
    request<void>(`/api/v1/admin/backups/${encodeURIComponent(id)}`, {
      method: "DELETE",
      headers: { "X-CSRF-Token": csrfToken },
    }),
  themes: () => request<{ items: ThemeRecord[] }>("/api/v1/admin/themes"),
  installTheme: (csrfToken: string, file: File, taskId?: string) => {
    const body = new FormData();
    body.append("file", file, file.name);
    return request<{ theme: ThemeRecord; build?: StaticBuildReport }>("/api/v1/admin/themes/install", {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body,
    });
  },
  installThemeURL: (csrfToken: string, url: string, taskId?: string) =>
    request<{ theme: ThemeRecord; build?: StaticBuildReport }>("/api/v1/admin/themes/install-url", {
      method: "POST",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify({ url }),
    }),
  activateTheme: (csrfToken: string, id: string, taskId?: string) =>
    request<{ activeTheme: string; report: StaticBuildReport }>(
      `/api/v1/admin/themes/${encodeURIComponent(id)}/activate`,
      { method: "POST", headers: trackedMutationHeaders(csrfToken, taskId) },
    ),
  reloadTheme: (csrfToken: string, id: string, taskId?: string) =>
    request<{ theme: ThemeRecord; build?: StaticBuildReport }>(
      `/api/v1/admin/themes/${encodeURIComponent(id)}/reload`,
      { method: "POST", headers: trackedMutationHeaders(csrfToken, taskId) },
    ),
  previewTheme: (csrfToken: string, id: string) =>
    request<ThemePreview>(`/api/v1/admin/themes/${encodeURIComponent(id)}/preview`, {
      method: "POST",
      headers: { "X-CSRF-Token": csrfToken },
    }),
  uninstallTheme: (csrfToken: string, id: string, deleteSettings = false) =>
    request<void>(`/api/v1/admin/themes/${encodeURIComponent(id)}?deleteSettings=${deleteSettings}`, {
      method: "DELETE",
      headers: { "X-CSRF-Token": csrfToken },
    }),
  themeSettings: (id: string) => request<ThemeSettings>(`/api/v1/admin/themes/${encodeURIComponent(id)}/settings`),
  saveThemeSettings: (csrfToken: string, id: string, values: Record<string, unknown>, taskId?: string) =>
    request<{ settings: ThemeSettings; build?: StaticBuildReport }>(
      `/api/v1/admin/themes/${encodeURIComponent(id)}/settings`,
      { method: "PUT", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify({ values }) },
    ),
  resetThemeSettings: (csrfToken: string, id: string, taskId?: string) =>
    request<{ settings: ThemeSettings; build?: StaticBuildReport }>(
      `/api/v1/admin/themes/${encodeURIComponent(id)}/settings/reset`,
      { method: "POST", headers: trackedMutationHeaders(csrfToken, taskId) },
    ),
  settings: () => request<{ site: SiteSettings; comments: CommentSettings }>("/api/v1/admin/settings"),
  updateSiteSettings: (
    csrfToken: string,
    body: {
      baseUrl: string;
      logo: string;
      timezone: string;
      adminLocale: "en" | "zh-CN";
      primaryMenu: string;
      idStrategy: "uuid" | "timestamp";
    },
    taskId?: string,
  ) =>
    request<{ site: SiteSettings; build: { status: string } }>("/api/v1/admin/settings/site", {
      method: "PUT",
      headers: trackedMutationHeaders(csrfToken, taskId),
      body: JSON.stringify(body),
    }),
  updateSiteLocale: (
    csrfToken: string,
    locale: string,
    body: { title: string; subtitle?: string; description?: string },
    taskId?: string,
  ) =>
    request<{ site: SiteSettings; build: { status: string } }>(
      `/api/v1/admin/settings/site/locales/${encodeURIComponent(locale)}`,
      { method: "PUT", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify(body) },
    ),
  updateCommentSettings: (csrfToken: string, body: CommentSettings, taskId?: string) =>
    request<{ comments: CommentSettings; build: { status: "succeeded" | "failed"; report?: StaticBuildReport } }>(
      "/api/v1/admin/settings/comments",
      { method: "PUT", headers: trackedMutationHeaders(csrfToken, taskId), body: JSON.stringify(body) },
    ),
  changePassword: (csrfToken: string, body: { currentPassword: string; newPassword: string }) =>
    request<{ status: "succeeded"; reauthenticate: true }>("/api/v1/admin/security/password", {
      method: "PUT",
      headers: { "X-CSRF-Token": csrfToken },
      body: JSON.stringify(body),
    }),
};
