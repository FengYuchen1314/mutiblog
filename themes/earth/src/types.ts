export interface ThemeLocale {
  code: string;
  label: string;
}

export interface ThemeSite {
  title: string;
  subtitle?: string;
  description?: string;
  locale: string;
  locales: ThemeLocale[];
}

export interface ThemePost {
  id: string;
  title: string;
  summary?: string;
  html: string;
  cover?: string;
  publishedAt?: string;
  categories?: Array<{ id: string; name: string }>;
  tags?: Array<{ id: string; name: string }>;
}

export interface ThemeContext {
  site: ThemeSite;
  currentPath: string;
  posts: ThemePost[];
  post?: ThemePost;
  strings: Record<string, string>;
  settings?: Record<string, unknown>;
}
