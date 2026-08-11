export interface ThemeLocale {
  code: string;
  label: string;
}

export interface ThemeSite {
  title: string;
  subtitle?: string;
  description?: string;
  locale: string;
  sourceLocale: string;
  locales: ThemeLocale[];
  baseUrl?: string;
}

export interface ThemePost {
  id: string;
  template?: string;
  title: string;
  summary?: string;
  seoTitle?: string;
  seoDescription?: string;
  html: string;
  cover?: string;
  publishedAt?: string;
  categories?: Array<{ id: string; name: string }>;
  tags?: Array<{ id: string; name: string }>;
  commentPolicy?: "open" | "closed";
}

export interface ThemeContext {
  site: ThemeSite;
  currentPath: string;
  posts: ThemePost[];
  post?: ThemePost;
  strings: Record<string, string>;
  settings?: Record<string, unknown>;
  navigation?: ThemeMenuItem[];
  pageTitle?: string;
  pageDescription?: string;
  canonicalUrl?: string;
  alternates?: Array<{ locale: string; label: string; href: string }>;
}

export interface ThemeMenuItem { id: string; label: string; href: string; openInNew?: boolean; children?: ThemeMenuItem[]; }

export interface ThemeLink { id: string; name: string; description?: string; url: string; logo?: string; }
export interface ThemeLinkGroup { id: string; name: string; description?: string; links: ThemeLink[]; }
