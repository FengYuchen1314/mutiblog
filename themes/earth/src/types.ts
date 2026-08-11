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
  timezone: string;
  locales: ThemeLocale[];
  baseUrl?: string;
  logo?: string;
  commentMaxLength: number;
}

export interface ThemePost {
  id: string;
  kind?: "post" | "page";
  template?: string;
  title: string;
  summary?: string;
  seoTitle?: string;
  seoDescription?: string;
  html: string;
  headings?: ThemeHeading[];
  cover?: string;
  pinned?: boolean;
  publishedAt?: string;
  categories?: Array<{ id: string; name: string }>;
  tags?: Array<{ id: string; name: string }>;
  commentPolicy?: "open" | "closed";
}

export interface ThemeHeading {
  id: string;
  level: 1 | 2 | 3 | 4;
  text: string;
}

export interface ThemePostCursor {
  previous?: ThemePost;
  next?: ThemePost;
}

export interface ThemePaginationPage {
  number: number;
  path: string;
  current: boolean;
}

export interface ThemePagination {
  page: number;
  pageSize: number;
  totalItems: number;
  totalPages: number;
  basePath: string;
  previousPath?: string;
  nextPath?: string;
  pages: ThemePaginationPage[];
}

export type ThemeTaxonomyKind = "categories" | "tags";

export interface ThemeTaxonomySummary {
  id: string;
  parentId?: string;
  name: string;
  description?: string;
  cover?: string;
  count: number;
  href: string;
}

export interface ThemeTaxonomyCollection {
  kind: ThemeTaxonomyKind;
  title: string;
  selectedId: string;
  items: ThemeTaxonomySummary[];
}

export interface ThemeTaxonomyCollections {
  categories: ThemeTaxonomySummary[];
  tags: ThemeTaxonomySummary[];
}

export interface ThemeContext {
  site: ThemeSite;
  currentPath: string;
  posts: ThemePost[];
  allPosts: ThemePost[];
  post?: ThemePost;
  cursor?: ThemePostCursor;
  taxonomy?: { kind: ThemeTaxonomyKind; id: string; cover?: string; template?: string; name: string; description?: string };
  taxonomyCollections: ThemeTaxonomyCollections;
  collection?: ThemeTaxonomyCollection;
  pagination?: ThemePagination;
  strings: Record<string, string>;
  settings?: Record<string, unknown>;
  navigation?: ThemeMenuItem[];
  menus?: ThemeMenu[];
  pageTitle?: string;
  pageDescription?: string;
  canonicalUrl?: string;
  alternates?: Array<{ locale: string; label: string; href: string }>;
}

export interface ThemeMenuItem { id: string; label: string; href: string; openInNew?: boolean; children?: ThemeMenuItem[]; }
export interface ThemeMenu { id: string; label: string; items: ThemeMenuItem[]; }

export interface ThemeLink { id: string; name: string; description?: string; url: string; logo?: string; }
export interface ThemeLinkGroup { id: string; name: string; description?: string; links: ThemeLink[]; }
