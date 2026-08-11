export interface LocalizedSiteInput {
  title: string;
  subtitle?: string;
  description?: string;
}

export interface LocaleInput {
  code: string;
  label: string;
  status?: "provisioning" | "building" | "ready" | "failed";
}

export interface LocalizedPostInput {
  title: string;
  summary?: string;
  seoTitle?: string;
  seoDescription?: string;
  markdown: string;
}

export interface PostInput {
  id: string;
  sourceLocale?: string;
  status: "published" | string;
  template?: string;
  cover?: string;
  pinned?: boolean;
  publishedAt?: string;
  categories?: string[];
  tags?: string[];
  commentPolicy?: "open" | "closed";
  locales: Record<string, LocalizedPostInput>;
}

export interface LocalizedTaxonomyInput {
  name: string;
  description?: string;
  seoTitle?: string;
  seoDescription?: string;
}
export interface TaxonomyInput { id: string; sourceLocale?: string; parentId?: string; cover?: string; template?: string; locales: Record<string, LocalizedTaxonomyInput>; }
export interface LocalizedLinkInput { name: string; description?: string; }
export interface LinkGroupInput { id: string; sourceLocale?: string; order: number; locales: Record<string, LocalizedLinkInput>; }
export interface LinkInput { id: string; sourceLocale?: string; groupId: string; url: string; logo?: string; order: number; locales: Record<string, LocalizedLinkInput>; }
export interface LocalizedMenuInput { label: string; }
export interface MenuItemInput { id: string; parentId?: string; targetKind: "internal" | "external"; url: string; openInNew: boolean; order: number; locales: Record<string, LocalizedMenuInput>; }
export interface MenuInput { id: string; sourceLocale?: string; locales: Record<string, LocalizedMenuInput>; items: MenuItemInput[]; }
export interface ContentTemplateInput { id: string; name: string; }
export interface ThemeInput { id: string; modulePath?: string; assetsPath?: string; settings?: Record<string, unknown>;
  localizedSettings?: Record<string, Record<string, string>>;
  localizableSettings?: string[];
  postTemplates?: ContentTemplateInput[]; pageTemplates?: ContentTemplateInput[]; categoryTemplates?: ContentTemplateInput[]; }

export interface BuildInput {
  schemaVersion: 1;
  sourceLocale: string;
  timezone?: string;
  baseUrl?: string;
  primaryMenu?: string;
  locales: LocaleInput[];
  site: { logo?: string; locales: Record<string, LocalizedSiteInput> };
  posts: PostInput[];
  pages?: PostInput[];
  categories?: TaxonomyInput[];
  tags?: TaxonomyInput[];
  linkGroups?: LinkGroupInput[];
  links?: LinkInput[];
  menus?: MenuInput[];
  dictionaries?: Record<string, Record<string, string>>;
  comments?: { moderation?: "pending" | "none"; pageSize?: number; maxLength?: number;
  };
  theme?: ThemeInput;
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
