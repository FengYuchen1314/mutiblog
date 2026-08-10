export interface LocalizedSiteInput {
  title: string;
  subtitle?: string;
  description?: string;
}

export interface LocaleInput {
  code: string;
  label: string;
}

export interface LocalizedPostInput {
  title: string;
  summary?: string;
  markdown: string;
}

export interface PostInput {
  id: string;
  status: "published" | string;
  cover?: string;
  publishedAt?: string;
  locales: Record<string, LocalizedPostInput>;
}

export interface BuildInput {
  schemaVersion: 1;
  sourceLocale: string;
  locales: LocaleInput[];
  site: { locales: Record<string, LocalizedSiteInput> };
  posts: PostInput[];
  fallback?: string[];
  generatedAt?: string;
}

export interface RedirectRecord {
  from: string;
  to: string;
  status: 302;
}

export interface BuildReport {
  schemaVersion: 1;
  generatedAt: string;
  files: number;
  locales: string[];
  redirects: RedirectRecord[];
}
