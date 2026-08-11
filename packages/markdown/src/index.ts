import MarkdownIt from "markdown-it";
import mathjax3 from "markdown-it-mathjax3";

export interface MarkdownHeading {
  id: string;
  level: 1 | 2 | 3 | 4;
  text: string;
}

export interface RenderedMarkdown {
  html: string;
  headings: MarkdownHeading[];
}

const renderer = new MarkdownIt({
  html: false,
  linkify: true,
  typographer: true,
});
renderer.use(mathjax3);
renderer.inline.ruler.before("math_inline", "currency_amount", (state, silent) => {
  if (state.src[state.pos] !== "$") return false;
  // The MathJax rule runs before Markdown-it's backtick rule. Without claiming
  // currency amounts first, the second dollar in prose such as
  // "$20,000 to $30,000. Use `$x$`" can pair with a dollar inside the later
  // code span. A mathematical operator immediately after the number keeps
  // expressions such as $2+2$ on the normal MathJax path.
  const amount = /^\$(?:0|[1-9]\d*)(?:,\d{3})*(?:\.\d{1,2})?(?=$|[\s.,;:!?)}\]])/.exec(state.src.slice(state.pos));
  if (!amount) return false;
  if (hasClosingMathDelimiter(state.src, state.pos, state.posMax)) return false;
  if (!silent) state.pending += amount[0];
  state.pos += amount[0].length;
  return true;
});

function hasClosingMathDelimiter(source: string, position: number, maximum: number) {
  let match = position + 1;
  while ((match = source.indexOf("$", match)) !== -1) {
    let previous = match - 1;
    while (source[previous] === "\\") previous -= 1;
    if ((match - previous) % 2 === 1) break;
    match += 1;
  }
  if (match <= position + 1 || match >= maximum) return false;
  const previous = source.charCodeAt(match - 1);
  const next = match + 1 < maximum ? source.charCodeAt(match + 1) : -1;
  return previous !== 32 && previous !== 9 && (next < 48 || next > 57);
}

export const markdownContentCSS = `
mjx-container.MathJax {
  max-width: 100%;
}
mjx-container.MathJax[display="true"] {
  display: block;
  max-width: 100%;
  overflow-x: auto;
  overflow-y: hidden;
  margin: 1.5rem 0;
  padding: .15rem 0;
  text-align: center;
}
mjx-container.MathJax > svg {
  max-width: none;
}
`;

export function renderMarkdown(source: string): string {
  return renderMarkdownWithHeadings(source).html;
}

/**
 * Render Markdown once, assigning stable h1–h4 anchors before rendering. The
 * returned data lets a theme build an accessible static table of contents
 * without parsing or mutating untrusted browser DOM.
 */
export function renderMarkdownWithHeadings(source: string): RenderedMarkdown {
  const environment: Record<string, unknown> = {};
  const tokens = renderer.parse(source, environment);
  const headings = annotateHeadings(tokens);
  const html = sanitizeGeneratedAttributes(renderer.renderer.render(tokens, renderer.options, environment));
  return { html, headings };
}

function annotateHeadings(tokens: ReturnType<typeof renderer.parse>): MarkdownHeading[] {
  const headings: MarkdownHeading[] = [];
  const occurrences = new Map<string, number>();
  for (let index = 0; index < tokens.length; index += 1) {
    const token = tokens[index];
    if (token.type !== "heading_open" || token.nesting !== 1 || !/^h[1-4]$/.test(token.tag)) continue;
    const level = Number(token.tag.slice(1)) as MarkdownHeading["level"];
    const text = headingText(tokens[index + 1]).trim();
    const base = headingSlug(text);
    const occurrence = (occurrences.get(base) ?? 0) + 1;
    occurrences.set(base, occurrence);
    const id = occurrence === 1 ? base : `${base}-${occurrence}`;
    token.attrSet("id", id);
    headings.push({ id, level, text: text || "Section" });
  }
  return headings;
}

function headingText(token: ReturnType<typeof renderer.parse>[number] | undefined): string {
  if (!token || token.type !== "inline") return "";
  const children = token.children ?? [];
  const text = children
    .filter((child) => !["link_open", "link_close", "em_open", "em_close", "strong_open", "strong_close"].includes(child.type))
    .map((child) => child.content)
    .join("");
  return text || token.content;
}

function headingSlug(text: string): string {
  const normalized = text
    .normalize("NFKD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-+|-+$/g, "");
  return normalized || "section";
}

function sanitizeGeneratedAttributes(html: string): string {
  return html.replace(/\s([:\w-]+)\s*=\s*("([^"]*)"|'([^']*)')/gi, (attribute, rawName: string, _quoted: string, doubleQuoted: string | undefined, singleQuoted: string | undefined) => {
    const name = rawName.toLowerCase();
    const value = decodeAttribute(doubleQuoted ?? singleQuoted ?? "");
    if (name.startsWith("on")) return "";
    if ((name === "href" || name === "xlink:href") && /^(?:javascript|vbscript|data):/i.test(value.replace(/[\u0000-\u0020]+/g, ""))) return "";
    if (name === "style" && /(?:expression\s*\(|url\s*\(\s*['"]?\s*(?:javascript|vbscript|data):)/i.test(value)) return "";
    return attribute;
  });
}

function decodeAttribute(value: string): string {
  return value
    .replace(/&#x([0-9a-f]+);?/gi, (_match, hex: string) => decodeCodePoint(hex, 16))
    .replace(/&#([0-9]+);?/g, (_match, decimal: string) => decodeCodePoint(decimal, 10))
    .replace(/&colon;/gi, ":")
    .replace(/&tab;/gi, "\t")
    .replace(/&newline;/gi, "\n");
}

function decodeCodePoint(value: string, radix: number): string {
  const codePoint = Number.parseInt(value, radix);
  return Number.isInteger(codePoint) && codePoint >= 0 && codePoint <= 0x10ffff ? String.fromCodePoint(codePoint) : "";
}
