const reactElement = Symbol.for("react.transitional.element");
const element = (type, props = {}, ...children) => {
  const normalizedProps = { ...props };
  if (children.length === 1) normalizedProps.children = children[0];
  else if (children.length > 1) normalizedProps.children = children;
  return {
    $$typeof: reactElement,
    type,
    key: null,
    props: normalizedProps,
    _owner: null,
  };
};

const document = (context, title, content = "") => element(
  "html",
  { lang: context.site.locale, style: { "--minimal-accent": context.settings?.appearance?.accent ?? "#7c3aed" } },
  element("head", {},
    element("meta", { charSet: "utf-8" }),
    element("meta", { name: "viewport", content: "width=device-width, initial-scale=1" }),
    element("title", {}, title),
    element("link", { rel: "stylesheet", href: "/assets/theme.css" }),
  ),
  element("body", {},
    element("header", {}, element("a", { href: `/${context.site.locale}/` }, context.site.title)),
    element("main", {}, element("h1", {}, title), element("p", {}, content)),
    element("footer", {}, "Minimal Theme for MutiBlog"),
  ),
);

export const css = "body{max-width:52rem;margin:3rem auto;padding:0 1rem;font:16px/1.7 system-ui;color:#202033}a{color:var(--minimal-accent)}";
export const renderIndex = (context) => document(context, context.site.title, `${context.posts.length} posts`);
export const renderPost = (context) => document(context, context.post.title, context.post.summary ?? "");
export const renderPage = (context) => document(context, context.post.title, context.post.summary ?? "");
export const pageTemplates = {
  landing: (context) => document(context, context.post.title, `Landing: ${context.post.summary ?? ""}`),
};
export const renderTaxonomy = (context, title, description) => document(context, title, description ?? "");
export const renderLinks = (context, groups) => document(context, context.strings.links, `${groups.length} groups`);
export const renderArchive = (context) => document(context, context.strings.archives, `${context.posts.length} posts`);
export const renderSearch = (context) => document(context, context.strings.search);
