import MarkdownIt from "markdown-it";
import type StateBlock from "markdown-it/lib/rules_block/state_block.mjs";
import type StateInline from "markdown-it/lib/rules_inline/state_inline.mjs";
import type Token from "markdown-it/lib/token.mjs";

const currencyAmount = /^\$(?:0|[1-9]\d*)(?:,\d{3})*(?:\.\d{1,2})?(?=$|[\s.,;:!?)}\]])/;
const unsafeTeX = /(?:\\(?:require|autoload|newcommand|renewcommand|newenvironment|renewenvironment|def|let|setoptions|href|url|class|cssid|style|data|htmlclass|htmlid|htmlstyle|htmldata|includegraphics)\b|(?:javascript|vbscript|data)\s*:)/i;

/**
 * Creates the browser-only Markdown renderer used by the editor preview.
 *
 * The public renderer turns TeX into SVG on the server. The editor must not
 * import that package because its Node-native MathJax bridge blocks browser
 * startup. Instead, these rules preserve the same dollar-delimiter behaviour
 * and leave a deliberately inert marker for the self-hosted browser MathJax
 * loader to replace later.
 */
export function createPreviewMarkdownRenderer() {
  const renderer = new MarkdownIt({ html: false, linkify: true, typographer: true });
  renderer.inline.ruler.after("escape", "math_inline", mathInline);
  renderer.block.ruler.after("blockquote", "math_block", mathBlock, {
    alt: ["paragraph", "reference", "blockquote", "list"],
  });
  renderer.inline.ruler.before("math_inline", "currency_amount", currencyInline);

  renderer.renderer.rules.math_inline = (tokens: Token[], index: number) => (
    `<span class="math-inline" data-math-preview="inline">${renderer.utils.escapeHtml(tokens[index].content)}</span>`
  );
  renderer.renderer.rules.math_block = (tokens: Token[], index: number) => (
    `<div class="math-display" data-math-preview="display">${renderer.utils.escapeHtml(tokens[index].content)}</div>\n`
  );

  return renderer;
}

function canUseDelimiter(state: StateInline, position: number) {
  const previous = position > 0 ? state.src.charCodeAt(position - 1) : -1;
  const next = position + 1 <= state.posMax ? state.src.charCodeAt(position + 1) : -1;
  return {
    canOpen: next !== 32 && next !== 9,
    canClose: previous !== 32 && previous !== 9 && (next < 48 || next > 57),
  };
}

function mathInline(state: StateInline, silent: boolean) {
  if (state.src[state.pos] !== "$") return false;

  let delimiter = canUseDelimiter(state, state.pos);
  if (!delimiter.canOpen) {
    if (!silent) state.pending += "$";
    state.pos += 1;
    return true;
  }

  const start = state.pos + 1;
  let match = start;
  while ((match = state.src.indexOf("$", match)) !== -1) {
    let previous = match - 1;
    while (state.src[previous] === "\\") previous -= 1;
    if ((match - previous) % 2 === 1) break;
    match += 1;
  }

  if (match === -1) {
    if (!silent) state.pending += "$";
    state.pos = start;
    return true;
  }
  if (match === start) {
    if (!silent) state.pending += "$$";
    state.pos = start + 1;
    return true;
  }

  delimiter = canUseDelimiter(state, match);
  if (!delimiter.canClose) {
    if (!silent) state.pending += "$";
    state.pos = start;
    return true;
  }

  const source = state.src.slice(start, match);
  if (!isSafeMathSource(source)) {
    // Preserve the complete delimiter pair as ordinary text. Returning false
    // would let the closing dollar be reconsidered against later prose.
    if (!silent) state.pending += state.src.slice(state.pos, match + 1);
    state.pos = match + 1;
    return true;
  }

  if (!silent) {
    const token = state.push("math_inline", "math", 0);
    token.markup = "$";
    token.content = source;
  }
  state.pos = match + 1;
  return true;
}

function mathBlock(state: StateBlock, start: number, end: number, silent: boolean) {
  let next: number;
  let lastPosition: number | undefined;
  let found = false;
  let position = state.bMarks[start] + state.tShift[start];
  let maximum = state.eMarks[start];
  let lastLine = "";

  if (position + 2 > maximum || state.src.slice(position, position + 2) !== "$$") return false;
  position += 2;
  let firstLine = state.src.slice(position, maximum);
  if (silent) return true;

  if (firstLine.trim().slice(-2) === "$$") {
    firstLine = firstLine.trim().slice(0, -2);
    found = true;
  }

  for (next = start; !found;) {
    next += 1;
    if (next >= end) break;
    position = state.bMarks[next] + state.tShift[next];
    maximum = state.eMarks[next];
    if (position < maximum && state.tShift[next] < state.blkIndent) break;
    if (state.src.slice(position, maximum).trim().slice(-2) === "$$") {
      lastPosition = state.src.slice(0, maximum).lastIndexOf("$$");
      lastLine = state.src.slice(position, lastPosition);
      found = true;
    }
  }

  const content = (firstLine && firstLine.trim() ? `${firstLine}\n` : "")
    + state.getLines(start + 1, next, state.tShift[start], true)
    + (lastLine && lastLine.trim() ? lastLine : "");
  if (!isSafeMathSource(content)) return false;

  state.line = next + 1;
  const token = state.push("math_block", "math", 0);
  token.block = true;
  token.content = content;
  token.map = [start, state.line];
  token.markup = "$$";
  return true;
}

function currencyInline(state: StateInline, silent: boolean) {
  if (state.src[state.pos] !== "$") return false;
  const amount = currencyAmount.exec(state.src.slice(state.pos));
  if (!amount) return false;
  if (!silent) state.pending += amount[0];
  state.pos += amount[0].length;
  return true;
}

/**
 * Preview formulae are authored by an administrator, but remain untrusted
 * Markdown at this boundary. The browser configuration disables these TeX
 * extensions too; rejecting them here makes that safety rule deterministic
 * and prevents an unsafe delimiter from being handed to MathJax at all.
 */
function isSafeMathSource(source: string) {
  return !unsafeTeX.test(source);
}
